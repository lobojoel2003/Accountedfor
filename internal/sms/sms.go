// Package sms lets people without a data connection use the board by text
// message. It parses commands such as "SAFE Maria Lopez, at the school
// shelter" and produces short replies that fit in a few SMS segments.
package sms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio signs webhooks with HMAC-SHA1.
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

// Kind is the type of an SMS command.
type Kind int

// Command kinds.
const (
	Help Kind = iota
	Safe
	Find
	Missing
	Delete
	// Stop is a carrier opt-out keyword. Gateways handle opt-outs
	// themselves; Accountedfor stays silent.
	Stop
)

// Command is a parsed text message.
type Command struct {
	Kind Kind
	// Lang is the language the keyword was written in.
	Lang       string
	Names      []string
	Message    string
	Visibility store.Visibility
}

type keyword struct {
	words string
	kind  Kind
	lang  string
}

// keywords are matched against the start of the message after accents are
// removed and letters upper-cased. Longer phrases come first.
var keywords = []keyword{
	{"ESTOY A SALVO", Safe, "es"},
	{"ESTOY BIEN", Safe, "es"},
	{"A SALVO", Safe, "es"},
	{"I'M SAFE", Safe, "en"},
	{"I AM SAFE", Safe, "en"},
	{"IM SAFE", Safe, "en"},
	{"IMSAFE", Safe, "en"},
	{"SAFE", Safe, "en"},
	{"SEGURO", Safe, "es"},
	{"SEGURA", Safe, "es"},
	{"FIND", Find, "en"},
	{"SEARCH", Find, "en"},
	{"BUSCAR", Find, "es"},
	{"BUSCA", Find, "es"},
	{"MISSING", Missing, "en"},
	{"LOST", Missing, "en"},
	{"DESAPARECIDO", Missing, "es"},
	{"DESAPARECIDA", Missing, "es"},
	{"DELETE", Delete, "en"},
	{"REMOVE", Delete, "en"},
	{"BORRAR", Delete, "es"},
	{"ELIMINAR", Delete, "es"},
	{"HELP", Help, "en"},
	{"INFO", Help, "en"},
	{"AYUDA", Help, "es"},
	{"STOP", Stop, "en"},
	{"STOPALL", Stop, "en"},
	{"UNSUBSCRIBE", Stop, "en"},
	{"CANCEL", Stop, "en"},
	{"END", Stop, "en"},
	{"QUIT", Stop, "en"},
	{"START", Stop, "en"},
	{"UNSTOP", Stop, "en"},
}

var modifiers = map[string]store.Visibility{
	"PRIVATE": store.Private, "PRIVADO": store.Private, "PRIVADA": store.Private,
	"PUBLIC": store.Public, "PUBLICO": store.Public, "PUBLICA": store.Public,
}

// upperFold upper-cases s and removes accents so "Público" matches PUBLICO.
func upperFold(s string) string {
	return strings.ToUpper(textutil.NormalizeName(s))
}

// Parse interprets a text message. Unknown messages are Help.
func Parse(body string) Command {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r", "\n"))
	body = strings.NewReplacer("’", "'", "‘", "'", "`", "'").Replace(body)
	cmd := Command{Kind: Help, Lang: "en"}
	if body == "" {
		return cmd
	}
	var rest string
	matched := false
	for _, k := range keywords {
		if len(body) < len(k.words) || !strings.EqualFold(body[:len(k.words)], k.words) {
			continue
		}
		after := body[len(k.words):]
		// The keyword must be a whole word.
		if after != "" {
			r := []rune(after)[0]
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		cmd.Kind, cmd.Lang, rest, matched = k.kind, k.lang, after, true
		break
	}
	if !matched {
		// Allow accented keywords, e.g. "BÚSCAR" typed by an autocorrect.
		first, after, _ := strings.Cut(body, " ")
		folded := upperFold(first)
		for _, k := range keywords {
			if k.words == folded {
				cmd.Kind, cmd.Lang, rest, matched = k.kind, k.lang, after, true
				break
			}
		}
	}
	if !matched {
		cmd.Lang = guessLang(body)
		return cmd
	}
	rest = strings.TrimLeft(rest, " \t\n:,.-!")

	// Visibility modifier right after the keyword: "SAFE PRIVATE Maria".
	if word, after, _ := strings.Cut(rest, " "); word != "" {
		if v, ok := modifiers[upperFold(word)]; ok {
			cmd.Visibility = v
			rest = strings.TrimSpace(after)
		}
	}

	namesPart, message := splitMessage(rest)
	cmd.Message = textutil.Multiline(message, 300)
	if cmd.Kind == Find {
		if n := textutil.Line(namesPart, 120); n != "" {
			cmd.Names = []string{n}
		}
		return cmd
	}
	cmd.Names = textutil.SplitNames(namesPart, 10, 120)
	return cmd
}

// splitMessage separates "Maria Lopez & Juan, at the school" into the names
// and the free-text message. Names end at the first comma, colon, line break
// or " - ".
func splitMessage(s string) (names, message string) {
	cut := len(s)
	for _, sep := range []string{",", ":", "\n", " - ", ";"} {
		if i := strings.Index(s, sep); i >= 0 && i < cut {
			cut = i
		}
	}
	if cut == len(s) {
		return s, ""
	}
	return s[:cut], strings.TrimLeft(s[cut:], ",:\n -;")
}

func guessLang(body string) string {
	u := " " + upperFold(body) + " "
	for _, w := range []string{" HOLA ", " ESTOY ", " AYUDA ", " BUSCO ", " MI ", " POR FAVOR "} {
		if strings.Contains(u, w) {
			return "es"
		}
	}
	return "en"
}

// Service executes commands against the store.
type Service struct {
	Store             *store.Store
	SiteName          string
	BaseURL           string
	DefaultVisibility store.Visibility
	Retention         time.Duration
	// Allow rate-limits senders; nil allows everything.
	Allow func(phone string) bool
}

// MaxReply keeps replies within three concatenated SMS segments.
const MaxReply = 459

// Handle runs the command in body sent from phone and returns the reply.
// An empty reply means none should be sent.
func (s *Service) Handle(ctx context.Context, from, body string) (string, error) {
	phone := textutil.NormalizePhone(from)
	cmd := Parse(body)
	lang := cmd.Lang
	t := func(key string, args ...any) string { return i18n.Tf(lang, key, args...) }

	if cmd.Kind == Stop {
		return "", nil
	}
	if s.Allow != nil && !s.Allow(phone) {
		return limit(t("sms.rate_limited")), nil
	}
	prefix := s.SiteName + ": "

	switch cmd.Kind {
	case Safe:
		if len(cmd.Names) == 0 {
			return limit(prefix + t("sms.safe_usage")), nil
		}
		vis := cmd.Visibility
		if vis == "" {
			vis = s.DefaultVisibility
		}
		var missing int
		for i, name := range cmd.Names {
			status := store.StatusIsNoteAuthor
			if i > 0 {
				status = store.StatusBelievedAlive
			}
			p := s.newPerson(name, cmd.Names[0], phone, vis)
			note := store.Note{
				Status:            status,
				AuthorMadeContact: true,
				AuthorName:        cmd.Names[0],
				AuthorPhone:       phone,
				Text:              cmd.Message,
				Origin:            store.OriginSMS,
			}
			if err := s.Store.CreatePerson(ctx, p, []store.Note{note}, ""); err != nil {
				return "", err
			}
			similar, err := s.Store.SimilarMissing(ctx, name, p.ID, 5)
			if err != nil {
				return "", err
			}
			missing += len(similar)
		}
		reply := prefix + t("sms.safe_done", strings.Join(cmd.Names, ", "), i18n.T(lang, "vis."+string(vis)))
		if missing > 0 {
			reply += " " + i18n.Tn(lang, "sms.safe_matches", missing)
		}
		reply += " " + t("sms.safe_footer")
		return limit(reply), nil

	case Missing:
		if len(cmd.Names) == 0 {
			return limit(prefix + t("sms.missing_usage")), nil
		}
		vis := cmd.Visibility
		if vis == "" {
			vis = s.DefaultVisibility
		}
		name := cmd.Names[0]
		p := s.newPerson(name, "", phone, vis)
		p.AuthorName = ""
		note := store.Note{
			Status:      store.StatusInformationSought,
			AuthorPhone: phone,
			Text:        cmd.Message,
			Origin:      store.OriginSMS,
		}
		if err := s.Store.CreatePerson(ctx, p, []store.Note{note}, ""); err != nil {
			return "", err
		}
		return limit(prefix + t("sms.missing_done", name)), nil

	case Find:
		if len(cmd.Names) == 0 || len([]rune(textutil.NormalizeName(cmd.Names[0]))) < 2 {
			return limit(prefix + t("sms.find_usage")), nil
		}
		return s.find(ctx, lang, cmd.Names[0])

	case Delete:
		n, err := s.Store.DeleteSMSPosts(ctx, phone)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return limit(prefix + t("sms.delete_none")), nil
		}
		return limit(prefix + i18n.Tn(lang, "sms.delete_done", n)), nil
	}
	return limit(prefix + t("sms.help")), nil
}

func (s *Service) newPerson(name, author, phone string, vis store.Visibility) *store.Person {
	p := &store.Person{
		FullName:    name,
		AuthorName:  author,
		AuthorPhone: phone,
		Visibility:  vis,
		Origin:      store.OriginSMS,
		SourceName:  s.SiteName + " SMS",
	}
	if s.Retention > 0 {
		exp := time.Now().UTC().Add(s.Retention)
		p.ExpiryDate = &exp
	}
	return p
}

func (s *Service) find(ctx context.Context, lang, name string) (string, error) {
	results, err := s.Store.Search(ctx, name, store.SearchOptions{Limit: 4})
	if err != nil {
		return "", err
	}
	prefix := s.SiteName + ": "
	if len(results) == 0 {
		return limit(prefix + i18n.Tf(lang, "sms.find_none", name)), nil
	}
	shown := results
	if len(shown) > 3 {
		shown = shown[:3]
	}
	ids := make([]int64, len(shown))
	for i := range shown {
		ids[i] = shown[i].ID
	}
	msgs, err := s.Store.LatestMessages(ctx, ids, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(prefix)
	for i, p := range shown {
		if i > 0 {
			b.WriteString(" | ")
		}
		cat := store.Category(p.PFIFStatus)
		if cat == "deceased" {
			cat = "contact"
		}
		fmt.Fprintf(&b, "%s: %s (%s)", p.FullName, i18n.T(lang, "sms.status_"+cat), shortAgo(lang, p.UpdatedAt))
		if n, ok := msgs[p.ID]; ok && p.Visibility == store.Public && n.Text != "" {
			fmt.Fprintf(&b, " \"%s\"", textutil.Truncate(n.Text, 60))
		}
	}
	if len(results) > len(shown) {
		b.WriteString(" | " + i18n.Tf(lang, "sms.find_more", s.BaseURL+"/search?q="+url.QueryEscape(name)))
	}
	return limit(b.String()), nil
}

func shortAgo(lang string, t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return i18n.T(lang, "time.just_now")
	case d < time.Hour:
		return i18n.Tn(lang, "time.minutes", int(d/time.Minute))
	case d < 48*time.Hour:
		return i18n.Tn(lang, "time.hours", int(d/time.Hour))
	}
	return i18n.Tn(lang, "time.days", int(d/(24*time.Hour)))
}

func limit(s string) string {
	r := []rune(s)
	if len(r) <= MaxReply {
		return s
	}
	return string(r[:MaxReply-1]) + "…"
}

// ValidTwilioSignature checks the X-Twilio-Signature header of a webhook
// request. fullURL must be the exact URL configured in Twilio, including
// any query string.
func ValidTwilioSignature(authToken, fullURL string, form url.Values, signature string) bool {
	if authToken == "" || signature == "" {
		return false
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		for _, v := range form[k] {
			b.WriteString(k)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// TwiML wraps a reply in Twilio's response XML.
func TwiML(reply string) string {
	if reply == "" {
		return `<?xml version="1.0" encoding="UTF-8"?><Response/>`
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Message>`)
	xmlEscape(&b, reply)
	b.WriteString(`</Message></Response>`)
	return b.String()
}

func xmlEscape(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
}

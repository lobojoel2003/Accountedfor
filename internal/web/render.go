package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

//go:embed templates static
var assets embed.FS

// pages maps a page name to its template file.
var pages = map[string]string{
	"home":                 "templates/home.html",
	"safe":                 "templates/safe.html",
	"missing":              "templates/missing.html",
	"posted":               "templates/posted.html",
	"search":               "templates/search.html",
	"person":               "templates/person.html",
	"edit":                 "templates/edit.html",
	"removed":              "templates/removed.html",
	"error":                "templates/error.html",
	"help":                 "templates/help.html",
	"privacy":              "templates/privacy.html",
	"responders/login":     "templates/responders/login.html",
	"responders/dashboard": "templates/responders/dashboard.html",
	"responders/matches":   "templates/responders/matches.html",
	"responders/new":       "templates/responders/new.html",
	"responders/data":      "templates/responders/data.html",
	"responders/users":     "templates/responders/users.html",
	"responders/audit":     "templates/responders/audit.html",
}

// Page holds what every page template needs. Page-specific data structs
// embed it.
type Page struct {
	Lang            string
	Title           string
	Site            string
	Event           string
	Notice          string
	Operator        string
	OperatorContact string
	SMSNumber       string
	BaseURL         string
	Version         string
	Build           string
	CSS             template.CSS
	Favicon         template.URL
	NoIndex         bool
	Wide            bool
	User            *store.User
	CSRF            string
	LangLinks       []langLink
	Errors          []string
}

func (p *Page) base() *Page { return p }

type pageData interface{ base() *Page }

type langLink struct {
	Code, Name, URL string
	Current         bool
}

// renderer holds one parsed template set per language, so templates can
// call {{t "key"}} without passing the language around.
type renderer struct {
	sets    map[string]map[string]*template.Template
	css     template.CSS
	cssHash string
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><circle cx="16" cy="16" r="15" fill="#0b6e4f"/><path d="M9 16.5l4.6 4.6L23.2 11" fill="none" stroke="#fff" stroke-width="3.4" stroke-linecap="round" stroke-linejoin="round"/></svg>`

var faviconURL = template.URL("data:image/svg+xml," + url.PathEscape(faviconSVG))

func newRenderer(loc *time.Location) (*renderer, error) {
	raw, err := assets.ReadFile("static/style.css")
	if err != nil {
		return nil, err
	}
	css := minifyCSS(string(raw))
	sum := sha256.Sum256([]byte(css))
	r := &renderer{
		sets:    map[string]map[string]*template.Template{},
		css:     template.CSS(css),
		cssHash: "sha256-" + base64.StdEncoding.EncodeToString(sum[:]),
	}
	for _, lang := range i18n.Languages {
		funcs := templateFuncs(lang.Code, loc)
		set := map[string]*template.Template{}
		for name, file := range pages {
			t, err := template.New(name).Funcs(funcs).ParseFS(assets,
				"templates/layout.html", "templates/responders/nav.html", file)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", file, err)
			}
			set[name] = t
		}
		r.sets[lang.Code] = set
	}
	return r, nil
}

func (r *renderer) render(w http.ResponseWriter, status int, name string, data pageData) error {
	p := data.base()
	set := r.sets[p.Lang]
	if set == nil {
		set = r.sets["en"]
	}
	t := set[name]
	if t == nil {
		return fmt.Errorf("unknown page %q", name)
	}
	p.CSS = r.css
	p.Favicon = faviconURL
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write(buf.Bytes())
	return err
}

var (
	cssComments = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssSpace    = regexp.MustCompile(`\s+`)
	cssPunct    = regexp.MustCompile(`\s*([{};,>])\s*`)
)

// minifyCSS strips comments and needless whitespace. The stylesheet is
// written to stay correct under these simple rules.
func minifyCSS(s string) string {
	s = cssComments.ReplaceAllString(s, "")
	s = cssSpace.ReplaceAllString(s, " ")
	s = cssPunct.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, ";}", "}")
	return strings.TrimSpace(s)
}

func toTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, !t.IsZero()
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, !t.IsZero()
	}
	return time.Time{}, false
}

func templateFuncs(lang string, loc *time.Location) template.FuncMap {
	return template.FuncMap{
		"t":  func(key string) string { return i18n.T(lang, key) },
		"tf": func(key string, args ...any) string { return i18n.Tf(lang, key, args...) },
		"tn": func(key string, n int) string { return i18n.Tn(lang, key, n) },
		"ago": func(v any) string {
			t, ok := toTime(v)
			if !ok {
				return ""
			}
			return ago(lang, t, time.Now(), loc)
		},
		"date": func(v any) string {
			t, ok := toTime(v)
			if !ok {
				return ""
			}
			return formatDate(t, loc)
		},
		"iso": func(v any) string {
			t, ok := toTime(v)
			if !ok {
				return ""
			}
			return t.UTC().Format(time.RFC3339)
		},
		"visKey":  func(v any) string { return "vis." + fmt.Sprint(v) },
		"phone":   textutil.NormalizePhone,
		"percent": func(f float64) string { return fmt.Sprintf("%.0f%%", f*100) },
	}
}

func formatDate(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2 Jan 2006, 15:04 MST")
}

func ago(lang string, t, now time.Time, loc *time.Location) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return i18n.T(lang, "time.just_now")
	case d < time.Hour:
		return i18n.Tn(lang, "time.minutes", int(d/time.Minute))
	case d < 24*time.Hour:
		return i18n.Tn(lang, "time.hours", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return i18n.Tn(lang, "time.days", int(d/(24*time.Hour)))
	}
	return t.In(loc).Format("2 Jan 2006")
}

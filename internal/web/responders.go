package web

import (
	"crypto/subtle"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/exchange"
	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/randid"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

// responder wraps handlers that need a signed-in responder. Unsafe methods
// also need the session's CSRF token.
func (s *Server) responder(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessionFrom(r) == nil {
			next := r.URL.Path
			if r.Method != http.MethodGet {
				next = "/responders"
			}
			s.redirect(w, r, "/responders/login?"+url.Values{"next": {next}}.Encode())
			return
		}
		if r.Method == http.MethodPost && !s.checkCSRF(w, r) {
			return
		}
		h(w, r)
	}
}

// admin wraps handlers that only administrators may use.
func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return s.responder(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsAdmin() {
			s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.admin_text")
			return
		}
		h(w, r)
	})
}

func (s *Server) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	sess := sessionFrom(r)
	token := r.PostFormValue("csrf")
	if sess == nil || subtle.ConstantTimeCompare([]byte(token), []byte(sess.CSRFToken)) != 1 {
		s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.csrf_text")
		return false
	}
	return true
}

func (s *Server) audit(r *http.Request, action, detail string) {
	actor := "system"
	if u := userFrom(r); u != nil {
		actor = u.Username
	}
	if err := s.store.Audit(r.Context(), actor, action, detail); err != nil {
		s.log.Error("audit", "err", err)
	}
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
}

type loginPage struct {
	*Page
	Username string
	Next     string
}

func safeNext(next string) string {
	if strings.HasPrefix(next, "/responders") && !strings.HasPrefix(next, "//") && !strings.Contains(next, "\\") {
		return next
	}
	return "/responders"
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if sessionFrom(r) != nil {
		s.redirect(w, r, "/responders")
		return
	}
	s.show(w, r, http.StatusOK, "responders/login", &loginPage{Page: s.page(r, "r.login_title")})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(s.clientIP(r)) {
		s.tooMany(w, r)
		return
	}
	username := textutil.Line(r.PostFormValue("username"), 40)
	u, err := s.store.Authenticate(r.Context(), username, r.PostFormValue("password"))
	if errors.Is(err, store.ErrNotFound) {
		p := s.page(r, "r.login_title")
		p.Errors = []string{"r.login_failed"}
		s.log.Warn("failed login", "username", store.NormalizeUsername(username), "ip", s.clientIP(r))
		s.show(w, r, http.StatusUnauthorized, "responders/login", &loginPage{Page: p, Username: username})
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	token, err := s.store.CreateSession(r.Context(), u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/",
		MaxAge: int(s.cfg.SessionTTL / time.Second), HttpOnly: true, Secure: s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode})
	if err := s.store.Audit(r.Context(), u.Username, "login", s.clientIP(r)); err != nil {
		s.log.Error("audit", "err", err)
	}
	s.redirect(w, r, safeNext(r.URL.Query().Get("next")))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	s.clearSessionCookie(w)
	s.redirect(w, r, "/")
}

func (s *Server) responderPage(r *http.Request, title string) *Page {
	p := s.page(r, title)
	p.Wide = true
	return p
}

// flashMessage turns a ?msg= code from a redirect into text.
func flashMessage(r *http.Request) string {
	lang := langFrom(r)
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("n"))
	switch q.Get("msg") {
	case "created":
		return i18n.Tn(lang, "r.msg_created", n)
	case "linked":
		return i18n.T(lang, "r.msg_linked")
	case "dismissed":
		return i18n.T(lang, "r.msg_dismissed")
	case "deleted":
		return i18n.T(lang, "r.msg_deleted")
	case "user":
		return i18n.T(lang, "r.msg_user")
	}
	return ""
}

type dashboardPage struct {
	*Page
	Stats        store.Stats
	MatchCount   int
	Filter       store.ListOptions
	Rows         []PersonView
	Total        int
	PrevURL      string
	NextURL      string
	Flash        string
	Statuses     []string
	Visibilities []string
	Origins      []string
}

const pageSize = 50

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	f := store.ListOptions{
		Query:      textutil.Line(q.Get("q"), maxName),
		Status:     oneOf(q.Get("status"), "safe", "missing", "unknown", "deceased"),
		Visibility: oneOf(q.Get("visibility"), "public", "limited", "private"),
		Origin:     oneOf(q.Get("origin"), store.OriginWeb, store.OriginSMS, store.OriginImport, store.OriginResponder),
		HiddenOnly: q.Get("hidden") == "1",
		Limit:      pageSize,
		Offset:     (page - 1) * pageSize,
	}
	people, total, err := s.store.List(r.Context(), f)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	stats, err := s.store.Stats(r.Context(), false)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	matches, err := s.store.Matches(r.Context(), 500)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := &dashboardPage{
		Page: s.responderPage(r, "r.dashboard_title"), Stats: stats, MatchCount: len(matches),
		Filter: f, Total: total, Flash: flashMessage(r),
		Statuses:     []string{"safe", "missing", "unknown", "deceased"},
		Visibilities: []string{"public", "limited", "private"},
		Origins:      []string{store.OriginWeb, store.OriginSMS, store.OriginImport, store.OriginResponder},
	}
	if err := s.fillViews(r, people, true, &data.Rows); err != nil {
		s.serverError(w, r, err)
		return
	}
	pageURL := func(n int) string {
		v := url.Values{}
		for k, vals := range q {
			if k != "page" && k != "msg" && k != "n" {
				v[k] = vals
			}
		}
		v.Set("page", strconv.Itoa(n))
		return "/responders?" + v.Encode()
	}
	if page > 1 {
		data.PrevURL = pageURL(page - 1)
	}
	if page*pageSize < total {
		data.NextURL = pageURL(page + 1)
	}
	s.show(w, r, http.StatusOK, "responders/dashboard", data)
}

func oneOf(v string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

type matchView struct {
	store.Match
	MissingMessage string
	SafeMessage    string
}

type matchesPage struct {
	*Page
	Matches []matchView
	Flash   string
}

func (s *Server) matches(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Matches(r.Context(), 100)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	ids := make([]int64, 0, 2*len(m))
	for _, x := range m {
		ids = append(ids, x.Missing.ID, x.Safe.ID)
	}
	msgs, err := s.store.LatestMessages(r.Context(), ids, true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	views := make([]matchView, len(m))
	for i, x := range m {
		views[i].Match = x
		if n, ok := msgs[x.Missing.ID]; ok {
			views[i].MissingMessage = listMessage(&x.Missing, &n, true)
		}
		if n, ok := msgs[x.Safe.ID]; ok {
			views[i].SafeMessage = listMessage(&x.Safe, &n, true)
		}
	}
	s.show(w, r, http.StatusOK, "responders/matches", &matchesPage{
		Page: s.responderPage(r, "r.matches_title"), Matches: views, Flash: flashMessage(r)})
}

func (s *Server) matchAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	missing, err1 := s.store.GetPerson(ctx, r.PostFormValue("missing"))
	safe, err2 := s.store.GetPerson(ctx, r.PostFormValue("safe"))
	if err := errors.Join(err1, err2); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	user := userFrom(r)
	lang := langFrom(r)
	switch r.PostFormValue("action") {
	case "link":
		n := &store.Note{
			Status:               store.StatusBelievedAlive,
			AuthorName:           user.Name(),
			LinkedPersonRecordID: safe.RecordID,
			Text:                 i18n.T(lang, "r.link_note"),
			Origin:               store.OriginResponder,
		}
		if err := s.store.AddNote(ctx, missing.ID, n); err != nil {
			s.serverError(w, r, err)
			return
		}
		back := &store.Note{
			AuthorName:           user.Name(),
			LinkedPersonRecordID: missing.RecordID,
			Text:                 i18n.T(lang, "r.link_back_note"),
			Origin:               store.OriginResponder,
			Private:              true,
		}
		if err := s.store.AddNote(ctx, safe.ID, back); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.audit(r, "match_link", missing.PublicID+" = "+safe.PublicID)
		s.redirect(w, r, "/responders/matches?msg=linked")
	case "dismiss":
		if err := s.store.DismissMatch(ctx, missing.ID, safe.ID); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.audit(r, "match_dismiss", missing.PublicID+" != "+safe.PublicID)
		s.redirect(w, r, "/responders/matches?msg=dismissed")
	default:
		s.redirect(w, r, "/responders/matches")
	}
}

type newRecordForm struct{ Names, Location, Text string }

type newRecordPage struct {
	*Page
	Form newRecordForm
}

func (s *Server) newRecordForm(w http.ResponseWriter, r *http.Request) {
	s.show(w, r, http.StatusOK, "responders/new", &newRecordPage{Page: s.responderPage(r, "r.new_title")})
}

// newRecordSubmit lets responders enter lists of names, for example from a
// shelter sign-in sheet.
func (s *Server) newRecordSubmit(w http.ResponseWriter, r *http.Request) {
	f := newRecordForm{
		Names:    r.PostFormValue("names"),
		Location: textutil.Line(r.PostFormValue("location"), 200),
		Text:     textutil.Multiline(r.PostFormValue("text"), maxText),
	}
	names := textutil.SplitNames(f.Names, 500, maxName)
	if len(names) == 0 {
		p := s.responderPage(r, "r.new_title")
		p.Errors = []string{"safe.error_name"}
		s.show(w, r, http.StatusUnprocessableEntity, "responders/new", &newRecordPage{Page: p, Form: f})
		return
	}
	status := r.PostFormValue("status")
	if status != store.StatusInformationSought {
		status = store.StatusBelievedAlive
	}
	vis := store.ParseVisibility(r.PostFormValue("visibility"), store.Limited)
	user := userFrom(r)
	for _, name := range names {
		p := &store.Person{
			FullName:   name,
			AuthorName: user.Name(),
			SourceName: s.cfg.SiteName + " responders",
			Visibility: vis,
			Origin:     store.OriginResponder,
			ExpiryDate: s.expiry(),
		}
		n := store.Note{
			Status:            status,
			AuthorMadeContact: status == store.StatusBelievedAlive,
			AuthorName:        user.Name(),
			LastKnownLocation: f.Location,
			Text:              f.Text,
			Origin:            store.OriginResponder,
		}
		if err := s.store.CreatePerson(r.Context(), p, []store.Note{n}, ""); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	s.audit(r, "create_records", fmt.Sprintf("%d records", len(names)))
	s.redirect(w, r, fmt.Sprintf("/responders?msg=created&n=%d", len(names)))
}

type dataPage struct {
	*Page
	PublicFeed bool
	Result     *exchange.ImportResult
}

func (s *Server) dataPage(w http.ResponseWriter, r *http.Request) {
	s.show(w, r, http.StatusOK, "responders/data", &dataPage{Page: s.responderPage(r, "r.data_title"), PublicFeed: s.cfg.PublicFeed})
}

const maxImport = 50 << 20

func (s *Server) importPFIF(w http.ResponseWriter, r *http.Request) {
	data := &dataPage{Page: s.responderPage(r, "r.data_title"), PublicFeed: s.cfg.PublicFeed}
	file, header, err := r.FormFile("file")
	if err != nil {
		data.Errors = []string{"r.import_no_file"}
		s.show(w, r, http.StatusUnprocessableEntity, "responders/data", data)
		return
	}
	defer file.Close()
	res, err := exchange.Import(r.Context(), s.store, http.MaxBytesReader(w, file, maxImport), exchange.ImportOptions{
		Visibility: store.ParseVisibility(r.PostFormValue("visibility"), store.Limited),
		Retention:  s.cfg.Retention(),
	})
	if err != nil {
		s.log.Warn("import failed", "err", err)
		data.Errors = []string{"r.import_failed"}
		data.Result = &exchange.ImportResult{Problems: []string{err.Error()}}
		s.show(w, r, http.StatusUnprocessableEntity, "responders/data", data)
		return
	}
	s.audit(r, "import_pfif", header.Filename+": "+res.Summary())
	data.Result = res
	s.show(w, r, http.StatusOK, "responders/data", data)
}

func attachment(w http.ResponseWriter, contentType, ext string) {
	name := "accountedfor-" + time.Now().UTC().Format("20060102-1504") + "." + ext
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
}

func (s *Server) exportPFIF(w http.ResponseWriter, r *http.Request) {
	attachment(w, "application/xml; charset=utf-8", "pfif.xml")
	n, err := exchange.Export(r.Context(), s.store, w, exchange.ExportOptions{Level: exchange.Full, Tombstones: true})
	if err != nil {
		s.log.Error("export", "err", err)
		return
	}
	s.audit(r, "export_pfif", fmt.Sprintf("%d records", n))
}

// csvSafe stops spreadsheet programs from running cell contents as
// formulas.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	attachment(w, "text/csv; charset=utf-8", "csv")
	// A byte order mark makes Excel read UTF-8 names correctly.
	_, _ = w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "name", "status", "pfif_status", "visibility", "hidden", "age", "sex",
		"home", "last_location", "latest_message", "author", "author_phone", "author_email", "origin",
		"source", "entered", "updated", "url", "pfif_record_id"})
	base := strings.TrimRight(s.cfg.BaseURL.String(), "/")
	n := 0
	err := s.store.Each(r.Context(), store.ExportFilter{IncludePrivate: true, IncludeHidden: true}, func(p *store.Person) error {
		var location, message string
		for i := len(p.Notes) - 1; i >= 0; i-- {
			if location == "" {
				location = p.Notes[i].LastKnownLocation
			}
			if message == "" {
				message = p.Notes[i].Text
			}
		}
		row := []string{p.PublicID, p.FullName, p.Status, p.PFIFStatus, string(p.Visibility),
			strconv.FormatBool(p.Hidden), p.Age, p.Sex, p.HomeArea(), location, message, p.AuthorName,
			p.AuthorPhone, p.AuthorEmail, p.Origin, p.SourceName, p.EntryDate.UTC().Format(time.RFC3339),
			p.UpdatedAt.UTC().Format(time.RFC3339), base + "/p/" + p.PublicID, p.RecordID}
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		n++
		return cw.Write(row)
	})
	cw.Flush()
	if err != nil {
		s.log.Error("csv export", "err", err)
		return
	}
	s.audit(r, "export_csv", fmt.Sprintf("%d records", n))
}

func (s *Server) moderatePerson(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPerson(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	back := "/p/" + p.PublicID
	switch action := r.PathValue("action"); action {
	case "hide", "unhide":
		err = s.store.SetPersonHidden(r.Context(), p.ID, action == "hide")
		s.audit(r, "person_"+action, p.PublicID+" "+p.FullName)
	case "visibility":
		p.Visibility = store.ParseVisibility(r.PostFormValue("visibility"), p.Visibility)
		err = s.store.UpdatePerson(r.Context(), p)
		s.audit(r, "person_visibility", p.PublicID+" "+string(p.Visibility))
	case "delete":
		if r.PostFormValue("confirm") != "1" {
			s.redirect(w, r, back)
			return
		}
		err = s.store.DeletePerson(r.Context(), p.ID)
		s.audit(r, "person_delete", p.PublicID+" "+p.FullName)
		back = "/responders?msg=deleted"
	default:
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, back)
}

func (s *Server) moderateNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	action := r.PathValue("action")
	if err != nil || (action != "hide" && action != "unhide") {
		s.notFound(w, r)
		return
	}
	personID, err := s.store.SetNoteHidden(r.Context(), id, action == "hide")
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p, err := s.store.GetPersonByID(r.Context(), personID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.audit(r, "note_"+action, fmt.Sprintf("%s note %d", p.PublicID, id))
	s.redirect(w, r, "/p/"+p.PublicID)
}

type usersPage struct {
	*Page
	Users       []store.User
	Tokens      []store.APIToken
	Flash       string
	Secret      string
	SecretLabel string
}

func (s *Server) showUsers(w http.ResponseWriter, r *http.Request, status int, secret, label string, errs []string) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	tokens, err := s.store.ListAPITokens(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.responderPage(r, "r.users_title")
	p.Errors = errs
	s.show(w, r, status, "responders/users", &usersPage{Page: p, Users: users, Tokens: tokens,
		Flash: flashMessage(r), Secret: secret, SecretLabel: label})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	s.showUsers(w, r, http.StatusOK, "", "", nil)
}

// NewPassword returns a random password that is easy to read out and type.
func NewPassword() string {
	raw := randid.Short(20)
	return raw[0:5] + "-" + raw[5:10] + "-" + raw[10:15] + "-" + raw[15:20]
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	username := store.NormalizeUsername(r.PostFormValue("username"))
	role := r.PostFormValue("role")
	if role != store.RoleAdmin {
		role = store.RoleResponder
	}
	password := NewPassword()
	_, err := s.store.CreateUser(r.Context(), username, textutil.Line(r.PostFormValue("display_name"), 80), password, role)
	if errors.Is(err, store.ErrExists) {
		s.showUsers(w, r, http.StatusConflict, "", "", []string{"r.user_exists"})
		return
	}
	if err != nil {
		s.showUsers(w, r, http.StatusUnprocessableEntity, "", "", []string{"r.user_invalid"})
		return
	}
	s.audit(r, "user_create", username+" ("+role+")")
	lang := langFrom(r)
	s.showUsers(w, r, http.StatusOK, password, i18n.Tf(lang, "r.password_for", username), nil)
}

func (s *Server) userAction(w http.ResponseWriter, r *http.Request) {
	username := store.NormalizeUsername(r.PathValue("username"))
	if username == userFrom(r).Username {
		s.showUsers(w, r, http.StatusUnprocessableEntity, "", "", []string{"r.user_self"})
		return
	}
	lang := langFrom(r)
	var err error
	switch action := r.PostFormValue("action"); action {
	case "reset":
		password := NewPassword()
		if err = s.store.SetPassword(r.Context(), username, password); err == nil {
			s.audit(r, "user_reset_password", username)
			s.showUsers(w, r, http.StatusOK, password, i18n.Tf(lang, "r.password_for", username), nil)
			return
		}
	case "disable", "enable":
		if err = s.store.SetUserDisabled(r.Context(), username, action == "disable"); err == nil {
			s.audit(r, "user_"+action, username)
			s.redirect(w, r, "/responders/users?msg=user")
			return
		}
	default:
		s.notFound(w, r)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	name := textutil.Line(r.PostFormValue("name"), 80)
	scope := r.PostFormValue("scope")
	if scope != "readwrite" {
		scope = "read"
	}
	t, secret, err := s.store.CreateAPIToken(r.Context(), name, scope)
	if err != nil {
		s.showUsers(w, r, http.StatusUnprocessableEntity, "", "", []string{"r.token_invalid"})
		return
	}
	s.audit(r, "token_create", fmt.Sprintf("%d %s (%s)", t.ID, t.Name, t.Scope))
	s.showUsers(w, r, http.StatusOK, secret, i18n.Tf(langFrom(r), "r.token_for", t.Name), nil)
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.store.RevokeAPIToken(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, err)
		return
	}
	s.audit(r, "token_revoke", strconv.FormatInt(id, 10))
	s.redirect(w, r, "/responders/users")
}

type auditPage struct {
	*Page
	Entries []store.AuditEntry
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListAudit(r.Context(), 300)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.show(w, r, http.StatusOK, "responders/audit", &auditPage{Page: s.responderPage(r, "r.audit_title"), Entries: entries})
}

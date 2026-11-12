package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/randid"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

const (
	maxName    = 120
	maxMessage = 500
	maxText    = 1000
	maxPeople  = 20
)

type visChoice struct {
	Visibility  string
	PublicHelp  string
	LimitedHelp string
}

func safeVis(v string) visChoice {
	return visChoice{Visibility: v, PublicHelp: "vis.public_help", LimitedHelp: "vis.limited_help"}
}

func missingVis(v string) visChoice {
	return visChoice{Visibility: v, PublicHelp: "vis.public_help_missing", LimitedHelp: "vis.limited_help_missing"}
}

// rejectSpam reports whether the hidden honeypot field was filled in, which
// only bots do. The caller pretends the post succeeded.
func rejectSpam(r *http.Request) bool { return r.PostFormValue("website") != "" }

// splitContact sorts free-form contact details into an email address or a
// phone number.
func splitContact(s string) (email, phone string) {
	s = textutil.Line(s, maxName)
	if s == "" {
		return "", ""
	}
	if strings.Contains(s, "@") {
		return s, ""
	}
	if p := textutil.NormalizePhone(s); len(strings.TrimPrefix(p, "+")) >= 5 {
		return "", p
	}
	return "", s
}

func (s *Server) expiry() *time.Time {
	if d := s.cfg.Retention(); d > 0 {
		t := time.Now().UTC().Add(d)
		return &t
	}
	return nil
}

func (s *Server) allowPost(w http.ResponseWriter, r *http.Request) bool {
	if userFrom(r) != nil {
		return true
	}
	if !s.postLimit.allow(s.clientIP(r)) {
		s.tooMany(w, r)
		return false
	}
	return true
}

type homePage struct {
	*Page
	Stats store.Stats
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context(), true)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.page(r, "")
	p.NoIndex = false
	s.show(w, r, http.StatusOK, "home", &homePage{Page: p, Stats: st})
}

type safeForm struct{ Name, Others, Message, Contact string }

type safePage struct {
	*Page
	Form safeForm
	Vis  visChoice
}

func (s *Server) safeForm(w http.ResponseWriter, r *http.Request) {
	s.show(w, r, http.StatusOK, "safe", &safePage{Page: s.page(r, "safe.title"), Vis: safeVis("limited")})
}

func (s *Server) safeSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allowPost(w, r) {
		return
	}
	if rejectSpam(r) {
		s.redirect(w, r, "/")
		return
	}
	f := safeForm{
		Name:    textutil.Line(r.PostFormValue("name"), maxName),
		Others:  textutil.Multiline(r.PostFormValue("others"), 1000),
		Message: textutil.Multiline(r.PostFormValue("message"), maxMessage),
		Contact: textutil.Line(r.PostFormValue("contact"), maxName),
	}
	vis := store.ParseVisibility(r.PostFormValue("visibility"), store.Limited)
	if textutil.NormalizeName(f.Name) == "" {
		p := s.page(r, "safe.title")
		p.Errors = []string{"safe.error_name"}
		s.show(w, r, http.StatusUnprocessableEntity, "safe", &safePage{Page: p, Form: f, Vis: safeVis(string(vis))})
		return
	}
	names := []string{f.Name}
	for _, n := range textutil.SplitNames(f.Others, maxPeople, maxName) {
		if textutil.NormalizeName(n) != textutil.NormalizeName(f.Name) {
			names = append(names, n)
		}
	}
	email, phone := splitContact(f.Contact)
	key := randid.Token()
	var ids []string
	for i, name := range names {
		status := store.StatusIsNoteAuthor
		if i > 0 {
			status = store.StatusBelievedAlive
		}
		p := &store.Person{
			FullName:    name,
			AuthorName:  f.Name,
			AuthorEmail: email,
			AuthorPhone: phone,
			Visibility:  vis,
			Origin:      store.OriginWeb,
			ExpiryDate:  s.expiry(),
		}
		note := store.Note{
			Status:            status,
			AuthorMadeContact: true,
			AuthorName:        f.Name,
			AuthorEmail:       email,
			AuthorPhone:       phone,
			Text:              f.Message,
			Origin:            store.OriginWeb,
		}
		if err := s.store.CreatePerson(r.Context(), p, []store.Note{note}, key); err != nil {
			s.serverError(w, r, err)
			return
		}
		ids = append(ids, p.PublicID)
	}
	s.redirect(w, r, postedURL(ids, key))
}

func postedURL(ids []string, key string) string {
	q := url.Values{"key": {key}}
	if len(ids) > 1 {
		q.Set("also", strings.Join(ids[1:], ","))
	}
	return "/p/" + ids[0] + "/posted?" + q.Encode()
}

type missingForm struct{ Name, Age, Home, LastSeen, Description, Author, Contact string }

type missingPage struct {
	*Page
	Form missingForm
	Vis  visChoice
}

func (s *Server) missingForm(w http.ResponseWriter, r *http.Request) {
	f := missingForm{Name: textutil.Line(r.URL.Query().Get("name"), maxName)}
	s.show(w, r, http.StatusOK, "missing", &missingPage{Page: s.page(r, "missing.title"), Form: f, Vis: missingVis("public")})
}

func (s *Server) missingSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allowPost(w, r) {
		return
	}
	if rejectSpam(r) {
		s.redirect(w, r, "/")
		return
	}
	f := missingForm{
		Name:        textutil.Line(r.PostFormValue("name"), maxName),
		Age:         textutil.Line(r.PostFormValue("age"), 20),
		Home:        textutil.Line(r.PostFormValue("home"), maxName),
		LastSeen:    textutil.Line(r.PostFormValue("last_seen"), 200),
		Description: textutil.Multiline(r.PostFormValue("description"), maxText),
		Author:      textutil.Line(r.PostFormValue("author"), maxName),
		Contact:     textutil.Line(r.PostFormValue("contact"), maxName),
	}
	vis := store.ParseVisibility(r.PostFormValue("visibility"), store.Public)
	var errs []string
	if textutil.NormalizeName(f.Name) == "" {
		errs = append(errs, "missing.error_name")
	}
	if f.Author == "" {
		errs = append(errs, "missing.error_author")
	}
	if len(errs) > 0 {
		p := s.page(r, "missing.title")
		p.Errors = errs
		s.show(w, r, http.StatusUnprocessableEntity, "missing", &missingPage{Page: p, Form: f, Vis: missingVis(string(vis))})
		return
	}
	email, phone := splitContact(f.Contact)
	key := randid.Token()
	p := &store.Person{
		FullName:    f.Name,
		Age:         f.Age,
		HomeCity:    f.Home,
		Description: f.Description,
		AuthorName:  f.Author,
		AuthorEmail: email,
		AuthorPhone: phone,
		Visibility:  vis,
		Origin:      store.OriginWeb,
		ExpiryDate:  s.expiry(),
	}
	note := store.Note{
		Status:            store.StatusInformationSought,
		AuthorName:        f.Author,
		AuthorEmail:       email,
		AuthorPhone:       phone,
		LastKnownLocation: f.LastSeen,
		Origin:            store.OriginWeb,
	}
	if err := s.store.CreatePerson(r.Context(), p, []store.Note{note}, key); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, postedURL([]string{p.PublicID}, key))
}

type matchGroup struct {
	Safe *store.Person
	// Self is true when the safe post was made by the person themselves,
	// false for family members posted by someone else.
	Self    bool
	Missing []store.Person
}

type postedPage struct {
	*Page
	People  []PersonView
	Missing bool
	Key     string
	Matches []matchGroup
}

// posted confirms a new post, shows the private edit link and offers
// matching missing-person reports ("Is someone looking for you?").
func (s *Server) posted(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	ids := []string{r.PathValue("id")}
	if also := r.URL.Query().Get("also"); also != "" {
		ids = append(ids, strings.Split(also, ",")...)
	}
	if len(ids) > maxPeople+1 {
		ids = ids[:maxPeople+1]
	}
	data := &postedPage{Page: s.page(r, "posted.title"), Key: key}
	for _, id := range ids {
		p, err := s.store.VerifyEditToken(r.Context(), id, key)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		data.People = append(data.People, newPersonView(p, false))
	}
	if len(data.People) == 0 {
		s.redirect(w, r, "/p/"+url.PathEscape(ids[0]))
		return
	}
	first := data.People[0].P
	data.Missing = first.Status == "missing"
	if !data.Missing {
		for _, v := range data.People {
			similar, err := s.store.SimilarMissing(r.Context(), v.P.FullName, v.P.ID, 5)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			if len(similar) > 0 {
				data.Matches = append(data.Matches, matchGroup{
					Safe: v.P, Self: v.P.PFIFStatus == store.StatusIsNoteAuthor, Missing: similar})
			}
		}
	}
	s.show(w, r, http.StatusOK, "posted", data)
}

type searchPage struct {
	*Page
	Query    string
	TooShort bool
	Results  []PersonView
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := textutil.Line(r.URL.Query().Get("q"), maxName)
	data := &searchPage{Page: s.page(r, "search.title"), Query: q}
	if q != "" {
		if len([]rune(textutil.NormalizeName(q))) < 2 {
			data.TooShort = true
		} else {
			responder := userFrom(r) != nil
			people, err := s.store.Search(r.Context(), q, store.SearchOptions{IncludePrivate: responder, Limit: 50})
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			if err := s.fillViews(r, people, responder, &data.Results); err != nil {
				s.serverError(w, r, err)
				return
			}
		}
	}
	s.show(w, r, http.StatusOK, "search", data)
}

// fillViews converts people to list views with a one-line latest message.
func (s *Server) fillViews(r *http.Request, people []store.Person, responder bool, out *[]PersonView) error {
	ids := make([]int64, len(people))
	for i := range people {
		ids[i] = people[i].ID
	}
	msgs, err := s.store.LatestMessages(r.Context(), ids, responder)
	if err != nil {
		return err
	}
	for i := range people {
		p := &people[i]
		v := newPersonView(p, responder)
		if n, ok := msgs[p.ID]; ok {
			v.Message = listMessage(p, &n, responder)
		}
		*out = append(*out, v)
	}
	return nil
}

type noteForm struct {
	Status, Text, Location, Author, Contact string
	Private                                 bool
}

type personPage struct {
	*Page
	View  PersonView
	Form  noteForm
	Added bool
}

// loadVisiblePerson returns the person if the viewer may see it. Hidden and
// private records answer 404 to the public so their existence is not
// revealed.
func (s *Server) loadVisiblePerson(w http.ResponseWriter, r *http.Request) (*store.Person, bool) {
	p, err := s.store.GetPerson(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !canSee(p, userFrom(r) != nil)) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	return p, true
}

func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadVisiblePerson(w, r)
	if !ok {
		return
	}
	s.showPerson(w, r, p, http.StatusOK, noteForm{}, nil)
}

func (s *Server) showPerson(w http.ResponseWriter, r *http.Request, p *store.Person, status int, f noteForm, errs []string) {
	responder := userFrom(r) != nil
	if err := s.store.LoadNotes(r.Context(), p, responder); err != nil {
		s.serverError(w, r, err)
		return
	}
	var linkedIDs []string
	for _, n := range p.Notes {
		if n.LinkedPersonRecordID != "" {
			linkedIDs = append(linkedIDs, n.LinkedPersonRecordID)
		}
	}
	linked, err := s.store.PersonsByRecordIDs(r.Context(), linkedIDs)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	v := newPersonView(p, responder)
	v.withNotes(linked)
	page := s.page(r, "")
	page.Title = p.FullName
	page.Errors = errs
	if f.Status == "" && f.Text == "" && len(errs) == 0 {
		f.Status = defaultNoteStatus(p)
	}
	s.show(w, r, status, "person", &personPage{Page: page, View: v, Form: f, Added: r.URL.Query().Get("added") == "1"})
}

func defaultNoteStatus(p *store.Person) string {
	if p.Status == "missing" {
		return store.StatusBelievedAlive
	}
	return ""
}

func (s *Server) addNote(w http.ResponseWriter, r *http.Request) {
	if !s.allowPost(w, r) {
		return
	}
	p, ok := s.loadVisiblePerson(w, r)
	if !ok {
		return
	}
	if rejectSpam(r) {
		s.redirect(w, r, "/p/"+p.PublicID)
		return
	}
	user := userFrom(r)
	if user != nil && !s.checkCSRF(w, r) {
		return
	}
	f := noteForm{
		Status:   r.PostFormValue("status"),
		Text:     textutil.Multiline(r.PostFormValue("text"), maxText),
		Location: textutil.Line(r.PostFormValue("location"), 200),
		Author:   textutil.Line(r.PostFormValue("author"), maxName),
		Contact:  textutil.Line(r.PostFormValue("contact"), maxName),
		Private:  r.PostFormValue("private") == "1",
	}
	allowed := map[string]bool{"": true, store.StatusIsNoteAuthor: true, store.StatusBelievedAlive: true,
		store.StatusInformationSought: true}
	if user != nil {
		allowed[store.StatusBelievedMissing] = true
		allowed[store.StatusBelievedDead] = true
		if f.Author == "" {
			f.Author = user.Name()
		}
	}
	var errs []string
	if !allowed[f.Status] {
		errs = append(errs, "note.error_status")
	}
	if f.Status == "" && f.Text == "" && f.Location == "" {
		errs = append(errs, "note.error_empty")
	}
	if f.Author == "" {
		errs = append(errs, "note.error_author")
	}
	if len(errs) > 0 {
		s.showPerson(w, r, p, http.StatusUnprocessableEntity, f, errs)
		return
	}
	email, phone := splitContact(f.Contact)
	origin := store.OriginWeb
	if user != nil {
		origin = store.OriginResponder
	}
	n := &store.Note{
		Status:            f.Status,
		AuthorMadeContact: f.Status == store.StatusIsNoteAuthor || f.Status == store.StatusBelievedAlive,
		AuthorName:        f.Author,
		AuthorEmail:       email,
		AuthorPhone:       phone,
		LastKnownLocation: f.Location,
		Text:              f.Text,
		Private:           f.Private,
		Origin:            origin,
	}
	if err := s.store.AddNote(r.Context(), p.ID, n); err != nil {
		s.serverError(w, r, err)
		return
	}
	if user != nil && f.Status == store.StatusBelievedDead {
		s.audit(r, "note_deceased", p.PublicID)
	}
	s.redirect(w, r, "/p/"+p.PublicID+"?added=1")
}

// thatsMe links a fresh "I'm safe" post to a missing-person report about
// the same person, so whoever is looking sees the news at once.
func (s *Server) thatsMe(w http.ResponseWriter, r *http.Request) {
	if !s.allowPost(w, r) {
		return
	}
	missing, ok := s.loadVisiblePerson(w, r)
	if !ok {
		return
	}
	safe, err := s.store.VerifyEditToken(r.Context(), r.PostFormValue("safe"), r.PostFormValue("key"))
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.key_text")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	lang := langFrom(r)
	// The safe post is either the person's own or one made for a family
	// member; the note says which.
	status, author, text := store.StatusIsNoteAuthor, safe.FullName, i18n.T(lang, "me.note_text")
	if safe.PFIFStatus != store.StatusIsNoteAuthor {
		status, author, text = store.StatusBelievedAlive, safe.AuthorName, i18n.T(lang, "me.note_text_them")
	}
	n := &store.Note{
		Status:               status,
		AuthorMadeContact:    true,
		AuthorName:           author,
		LinkedPersonRecordID: safe.RecordID,
		Text:                 text,
		Origin:               store.OriginWeb,
	}
	if err := s.store.AddNote(r.Context(), missing.ID, n); err != nil {
		s.serverError(w, r, err)
		return
	}
	back := &store.Note{
		LinkedPersonRecordID: missing.RecordID,
		AuthorName:           author,
		Text:                 i18n.T(lang, "me.back_text"),
		Origin:               store.OriginWeb,
	}
	if err := s.store.AddNote(r.Context(), safe.ID, back); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, "/p/"+missing.PublicID+"?added=1")
}

type editPage struct {
	*Page
	P     *store.Person
	Key   string
	Vis   visChoice
	Saved bool
}

func (s *Server) editForm(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	p, err := s.store.VerifyEditToken(r.Context(), r.PathValue("id"), key)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.key_text")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.showEdit(w, r, p, key, http.StatusOK, nil)
}

func (s *Server) showEdit(w http.ResponseWriter, r *http.Request, p *store.Person, key string, status int, errs []string) {
	vis := safeVis(string(p.Visibility))
	if p.Status == "missing" {
		vis = missingVis(string(p.Visibility))
	}
	page := s.page(r, "edit.title")
	page.Errors = errs
	s.show(w, r, status, "edit", &editPage{Page: page, P: p, Key: key, Vis: vis, Saved: r.URL.Query().Get("saved") == "1"})
}

func (s *Server) editSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allowPost(w, r) {
		return
	}
	key := r.PostFormValue("key")
	p, err := s.store.VerifyEditToken(r.Context(), r.PathValue("id"), key)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.key_text")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	editURL := "/p/" + p.PublicID + "/edit?" + url.Values{"key": {key}, "saved": {"1"}}.Encode()
	switch r.PostFormValue("action") {
	case "save":
		name := textutil.Line(r.PostFormValue("name"), maxName)
		if textutil.NormalizeName(name) == "" {
			s.showEdit(w, r, p, key, http.StatusUnprocessableEntity, []string{"safe.error_name"})
			return
		}
		p.FullName = name
		p.Visibility = store.ParseVisibility(r.PostFormValue("visibility"), p.Visibility)
		if err := s.store.UpdatePerson(r.Context(), p); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, editURL)
	case "update":
		text := textutil.Multiline(r.PostFormValue("text"), maxText)
		location := textutil.Line(r.PostFormValue("location"), 200)
		if text == "" && location == "" {
			s.showEdit(w, r, p, key, http.StatusUnprocessableEntity, []string{"note.error_empty"})
			return
		}
		status := p.PFIFStatus
		if p.Status == "missing" {
			if st := r.PostFormValue("status"); st == store.StatusBelievedAlive || st == store.StatusInformationSought {
				status = st
			}
		}
		n := &store.Note{
			Status:            status,
			AuthorMadeContact: status == store.StatusIsNoteAuthor || status == store.StatusBelievedAlive,
			AuthorName:        p.AuthorName,
			Text:              text,
			LastKnownLocation: location,
			Origin:            store.OriginWeb,
		}
		if err := s.store.AddNote(r.Context(), p.ID, n); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, editURL)
	case "delete":
		if r.PostFormValue("confirm") != "1" {
			s.showEdit(w, r, p, key, http.StatusUnprocessableEntity, []string{"edit.error_confirm"})
			return
		}
		if err := s.store.DeletePerson(r.Context(), p.ID); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.show(w, r, http.StatusOK, "removed", s.page(r, "removed.title"))
	default:
		s.redirect(w, r, editURL)
	}
}

type privacyPage struct {
	*Page
	RetentionDays int
}

func (s *Server) privacy(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "privacy.title")
	p.NoIndex = false
	s.show(w, r, http.StatusOK, "privacy", &privacyPage{Page: p, RetentionDays: s.cfg.RetentionDays})
}

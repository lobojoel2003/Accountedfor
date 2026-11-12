package web_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/lobojoel2003/accountedfor/internal/config"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/testdb"
	"github.com/lobojoel2003/accountedfor/internal/web"
)

// Page weight budget for the home page, the page people open on a failing
// network. CI fails if a change makes it heavier.
const (
	budgetRaw  = 12 * 1024
	budgetGzip = 5 * 1024
)

type env struct {
	t      *testing.T
	srv    *httptest.Server
	st     *store.Store
	client *http.Client
}

func setup(t *testing.T, extra map[string]string) *env {
	t.Helper()
	st := testdb.Open(t, "web")
	vars := map[string]string{
		"SITE_NAME":          "Test Board",
		"EVENT_NAME":         "Test Flood",
		"SMS_NUMBER":         "+15550100",
		"SMS_WEBHOOK_SECRET": "webhook-secret",
		"TWILIO_AUTH_TOKEN":  "twilio-token",
	}
	for k, v := range extra {
		vars[k] = v
	}
	// BASE_URL must match the test server, so start it first and swap in
	// the handler.
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	vars["BASE_URL"] = srv.URL
	cfg, err := config.LoadFrom(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	s, err := web.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	handler = s.Handler()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &env{t: t, srv: srv, st: st, client: client}
}

type resp struct {
	status   int
	body     string
	header   http.Header
	location string
}

func (e *env) do(req *http.Request) resp {
	e.t.Helper()
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: string(b), header: res.Header, location: res.Header.Get("Location")}
}

func (e *env) get(path string) resp {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	return e.do(req)
}

// post submits a form the way a browser on the same site does.
func (e *env) post(path string, form url.Values) resp {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.srv.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return e.do(req)
}

func expect(t *testing.T, r resp, status int, contains ...string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d\n%s", r.status, status, snippet(r.body))
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			t.Errorf("body lacks %q\n%s", c, snippet(r.body))
		}
	}
}

func refute(t *testing.T, r resp, absent ...string) {
	t.Helper()
	for _, a := range absent {
		if strings.Contains(r.body, a) {
			t.Errorf("body must not contain %q", a)
		}
	}
}

func snippet(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

var publicIDRe = regexp.MustCompile(`/p/([a-z0-9]{10})`)

func TestHomePageWeightAndSafety(t *testing.T) {
	e := setup(t, nil)
	r := e.get("/")
	expect(t, r, http.StatusOK, "I&#39;m safe", "Test Flood", "&#43;15550100", `href="sms:&#43;15550100?body=SAFE%20"`)
	if len(r.body) > budgetRaw {
		t.Errorf("home page is %d bytes, budget %d", len(r.body), budgetRaw)
	}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	compressed, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("home page not gzip-compressed")
	}
	if len(compressed) > budgetGzip {
		t.Errorf("home page is %d bytes gzipped, budget %d", len(compressed), budgetGzip)
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != r.body {
		t.Error("gzip body differs from plain body")
	}
	t.Logf("home page: %d bytes, %d gzipped", len(r.body), len(compressed))

	// One request: no scripts, stylesheets, fonts or images to fetch.
	for _, banned := range []string{"<script", "<link rel=\"stylesheet\"", "<img", "@import", "url("} {
		if strings.Contains(r.body, banned) {
			t.Errorf("home page contains %q", banned)
		}
	}
	// The CSP allows exactly the inline stylesheet.
	csp := r.header.Get("Content-Security-Policy")
	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(r.body)
	if len(m) != 2 {
		t.Fatal("no inline style")
	}
	sum := sha256.Sum256([]byte(m[1]))
	if !strings.Contains(csp, "sha256-"+base64.StdEncoding.EncodeToString(sum[:])) || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP does not allow the inline style: %s", csp)
	}
	for h, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := r.header.Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
}

var rawKey = regexp.MustCompile(`\b(home|safe|missing|posted|search|person|note|edit|removed|error|help|privacy|vis|status|badge|sms|time|me|form|nav|footer|meta|origin|role|sex|r)\.[a-z_]+\b`)

func TestAllPagesTranslated(t *testing.T) {
	e := setup(t, nil)
	p := &store.Person{FullName: "Test Person", Visibility: store.Public, Origin: store.OriginWeb, Age: "30"}
	if err := e.st.CreatePerson(context.Background(), p, []store.Note{{Status: store.StatusInformationSought, Text: "hi", Origin: store.OriginWeb}}, "k"); err != nil {
		t.Fatal(err)
	}
	pages := []string{"/", "/safe", "/missing", "/search", "/search?q=test", "/search?q=zzzzzz", "/search?q=a",
		"/help", "/privacy", "/p/" + p.PublicID, "/p/" + p.PublicID + "/posted?key=k", "/p/" + p.PublicID + "/edit?key=k",
		"/responders/login", "/nope"}
	for _, lang := range []string{"en", "es"} {
		for _, path := range pages {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			r := e.get(path + sep + "lang=" + lang)
			if r.status >= 500 {
				t.Errorf("%s (%s): status %d", path, lang, r.status)
			}
			if !strings.Contains(r.body, `<html lang="`+lang+`">`) {
				t.Errorf("%s: wrong language", path)
			}
			html := regexp.MustCompile(`(?s)<style>.*?</style>`).ReplaceAllString(r.body, "")
			if strings.Contains(html, "%!") {
				t.Errorf("%s (%s): formatting error in page", path, lang)
			}
			if k := rawKey.FindString(html); k != "" {
				t.Errorf("%s (%s): untranslated key %q", path, lang, k)
			}
		}
	}
	// Language choice sticks in a cookie and Accept-Language is honored.
	expect(t, e.get("/"), http.StatusOK, `lang="es"`)
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/", nil)
	req.Header.Set("Accept-Language", "es-MX,es;q=0.9")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "Estoy a salvo") {
		t.Error("Accept-Language not honored")
	}
}

func TestSafeFlowLimitedVisibility(t *testing.T) {
	e := setup(t, nil)
	r := e.post("/safe", url.Values{
		"name":       {"María López"},
		"others":     {"Juan López\nAna López"},
		"message":    {"At the Lincoln High shelter"},
		"visibility": {"limited"},
		"contact":    {"+1 555 0199"},
	})
	if r.status != http.StatusSeeOther || !strings.Contains(r.location, "/posted?") {
		t.Fatalf("post: %d %s", r.status, r.location)
	}
	loc, _ := url.Parse(r.location)
	key := loc.Query().Get("key")
	posted := e.get(r.location)
	expect(t, posted, http.StatusOK, "María López", "Juan López", "Ana López", "edit?key="+key)
	id := publicIDRe.FindStringSubmatch(r.location)[1]

	// The public sees the name and status but not the message or contact.
	page := e.get("/p/" + id)
	expect(t, page, http.StatusOK, "María López", "Posted by this person")
	refute(t, page, "Lincoln High", "0199")
	search := e.get("/search?q=maria+lopez")
	expect(t, search, http.StatusOK, "María López")
	refute(t, search, "Lincoln High")

	// The edit link works; a wrong key does not.
	expect(t, e.get("/p/"+id+"/edit?key=wrong"), http.StatusForbidden)
	expect(t, e.get("/p/"+id+"/edit?key="+key), http.StatusOK, "Change your post")
	r = e.post("/p/"+id+"/edit", url.Values{"key": {key}, "action": {"save"}, "name": {"Maria Lopez Garcia"}, "visibility": {"public"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("save: %d", r.status)
	}
	expect(t, e.get("/p/"+id), http.StatusOK, "Maria Lopez Garcia", "Lincoln High")
	r = e.post("/p/"+id+"/edit", url.Values{"key": {key}, "action": {"update"}, "text": {"Moved to my sister's house"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("update: %d", r.status)
	}
	expect(t, e.get("/p/"+id), http.StatusOK, "Moved to my sister")

	// Deleting needs confirmation.
	expect(t, e.post("/p/"+id+"/edit", url.Values{"key": {key}, "action": {"delete"}}), http.StatusUnprocessableEntity)
	expect(t, e.post("/p/"+id+"/edit", url.Values{"key": {key}, "action": {"delete"}, "confirm": {"1"}}), http.StatusOK, "Post removed")
	expect(t, e.get("/p/"+id), http.StatusNotFound)
}

func TestSafeValidationAndSpam(t *testing.T) {
	e := setup(t, nil)
	expect(t, e.post("/safe", url.Values{"name": {"  !! "}}), http.StatusUnprocessableEntity, "Please enter a name")
	r := e.post("/safe", url.Values{"name": {"Bot"}, "website": {"http://spam.example"}})
	if r.status != http.StatusSeeOther || r.location != "/" {
		t.Fatalf("honeypot: %d %s", r.status, r.location)
	}
	if people, _ := e.st.Search(context.Background(), "bot", store.SearchOptions{IncludePrivate: true}); len(people) != 0 {
		t.Fatal("honeypot post was stored")
	}
}

func TestCrossSiteFormsBlocked(t *testing.T) {
	e := setup(t, nil)
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/safe", strings.NewReader("name=Evil"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	expect(t, e.do(req), http.StatusForbidden)
}

func TestMissingReportNotesAndReunion(t *testing.T) {
	e := setup(t, nil)
	r := e.post("/missing", url.Values{"name": {"Carlos Vega"}, "age": {"70"}, "home": {"Riverside"},
		"last_seen": {"the bridge"}, "author": {"Lucia Vega"}, "contact": {"lucia@example.org"}, "visibility": {"public"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("missing: %d", r.status)
	}
	missingID := publicIDRe.FindStringSubmatch(r.location)[1]
	page := e.get("/p/" + missingID)
	expect(t, page, http.StatusOK, "Carlos Vega", "Being looked for", "the bridge", "70")
	refute(t, page, "lucia@example.org")

	expect(t, e.post("/missing", url.Values{"name": {"X"}}), http.StatusUnprocessableEntity, "Please enter your name")
	expect(t, e.post("/p/"+missingID+"/notes", url.Values{"author": {"A"}}), http.StatusUnprocessableEntity)
	expect(t, e.post("/p/"+missingID+"/notes", url.Values{"author": {"A"}, "status": {"believed_dead"}}), http.StatusUnprocessableEntity)

	// Carlos marks himself safe and answers the report.
	r = e.post("/safe", url.Values{"name": {"Carlos Vega"}, "visibility": {"limited"}})
	loc, _ := url.Parse(r.location)
	safeID := publicIDRe.FindStringSubmatch(r.location)[1]
	posted := e.get(r.location)
	expect(t, posted, http.StatusOK, "Is someone looking for Carlos Vega?", "/p/"+missingID+"/me")
	expect(t, e.post("/p/"+missingID+"/me", url.Values{"safe": {safeID}, "key": {"wrong"}}), http.StatusForbidden)
	r = e.post("/p/"+missingID+"/me", url.Values{"safe": {safeID}, "key": {loc.Query().Get("key")}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("that's me: %d", r.status)
	}
	expect(t, e.get("/p/"+missingID), http.StatusOK, "badge safe", "I&#39;m safe. See my post.", "/p/"+safeID)

	// Anyone can add news; private notes stay private.
	r = e.post("/p/"+missingID+"/notes", url.Values{"status": {""}, "text": {"Secret detail"}, "author": {"Neighbor"}, "private": {"1"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("note: %d", r.status)
	}
	refute(t, e.get("/p/"+missingID), "Secret detail")
}

func TestPrivateRecordsAreHidden(t *testing.T) {
	e := setup(t, nil)
	r := e.post("/safe", url.Values{"name": {"Hidden Person"}, "visibility": {"private"}, "message": {"room 12"}})
	id := publicIDRe.FindStringSubmatch(r.location)[1]
	expect(t, e.get("/p/"+id), http.StatusNotFound)
	refute(t, e.get("/search?q=hidden+person"), "Hidden Person")
	refute(t, e.get("/pfif.xml"), "Hidden Person")
	refute(t, e.get("/"), "1 person marked safe")

	login(t, e, "admin", store.RoleAdmin)
	expect(t, e.get("/p/"+id), http.StatusOK, "Hidden Person", "room 12")
	expect(t, e.get("/search?q=hidden+person"), http.StatusOK, "Hidden Person")
}

func login(t *testing.T, e *env, username, role string) string {
	t.Helper()
	if _, err := e.st.CreateUser(context.Background(), username, "", "correct-horse-battery", role); err != nil {
		t.Fatal(err)
	}
	expect(t, e.post("/responders/login", url.Values{"username": {username}, "password": {"nope-nope-nope"}}), http.StatusUnauthorized, "Wrong username")
	r := e.post("/responders/login", url.Values{"username": {username}, "password": {"correct-horse-battery"}})
	if r.status != http.StatusSeeOther || r.location != "/responders" {
		t.Fatalf("login: %d %s", r.status, r.location)
	}
	dash := e.get("/responders")
	expect(t, dash, http.StatusOK, "Responder dashboard")
	m := regexp.MustCompile(`name="csrf" value="([a-z0-9]+)"`).FindStringSubmatch(dash.body)
	if m == nil {
		t.Fatal("no CSRF token on dashboard")
	}
	return m[1]
}

func TestResponderArea(t *testing.T) {
	e := setup(t, nil)
	r := e.get("/responders")
	if r.status != http.StatusSeeOther || !strings.HasPrefix(r.location, "/responders/login") {
		t.Fatalf("anonymous dashboard: %d %s", r.status, r.location)
	}
	ctx := context.Background()
	missing := &store.Person{FullName: "Maria Lopez", Visibility: store.Public, Origin: store.OriginWeb, AuthorPhone: "+15550199"}
	if err := e.st.CreatePerson(ctx, missing, []store.Note{{Status: store.StatusInformationSought, Origin: store.OriginWeb}}, ""); err != nil {
		t.Fatal(err)
	}
	safe := &store.Person{FullName: "María López", Visibility: store.Limited, Origin: store.OriginSMS}
	if err := e.st.CreatePerson(ctx, safe, []store.Note{{Status: store.StatusIsNoteAuthor, Origin: store.OriginSMS}}, ""); err != nil {
		t.Fatal(err)
	}

	csrf := login(t, e, "resp", store.RoleResponder)
	// html/template writes "+" as "&#43;".
	expect(t, e.get("/responders"), http.StatusOK, "&#43;15550199", "Maria Lopez")
	expect(t, e.get("/responders?status=missing&q=maria"), http.StatusOK, "1 record")
	expect(t, e.get("/responders/matches"), http.StatusOK, "100% name match")
	expect(t, e.get("/responders/users"), http.StatusForbidden)
	expect(t, e.get("/responders/data"), http.StatusOK, "Download CSV")

	// Unsafe actions need the CSRF token.
	form := url.Values{"missing": {missing.PublicID}, "safe": {safe.PublicID}, "action": {"link"}}
	expect(t, e.post("/responders/matches", form), http.StatusForbidden)
	form.Set("csrf", csrf)
	if r := e.post("/responders/matches", form); r.status != http.StatusSeeOther {
		t.Fatalf("link: %d", r.status)
	}
	expect(t, e.get("/p/"+missing.PublicID), http.StatusOK, "badge safe", "A responder matched")

	csv := e.get("/responders/export.csv")
	expect(t, csv, http.StatusOK, "María López", "+15550199")
	if !strings.HasPrefix(csv.header.Get("Content-Disposition"), "attachment") {
		t.Error("CSV is not an attachment")
	}
	expect(t, e.get("/responders/export.xml"), http.StatusOK, "<pfif:pfif", "+15550199")

	// Moderation.
	if r := e.post("/responders/person/"+safe.PublicID+"/hide", url.Values{"csrf": {csrf}}); r.status != http.StatusSeeOther {
		t.Fatalf("hide: %d", r.status)
	}
	got, _ := e.st.GetPerson(ctx, safe.PublicID)
	if !got.Hidden {
		t.Fatal("record not hidden")
	}

	// Responders can record deaths; the public never sees the word.
	if r := e.post("/p/"+missing.PublicID+"/notes", url.Values{"csrf": {csrf}, "status": {"believed_dead"}, "text": {"Confirmed by hospital"}}); r.status != http.StatusSeeOther {
		t.Fatalf("responder note: %d", r.status)
	}
	expect(t, e.get("/p/"+missing.PublicID), http.StatusOK, "Reported deceased")

	entries, _ := e.st.ListAudit(ctx, 50)
	var actions []string
	for _, a := range entries {
		actions = append(actions, a.Action)
	}
	sort.Strings(actions)
	for _, want := range []string{"login", "match_link", "export_csv", "export_pfif", "person_hide", "note_deceased"} {
		i := sort.SearchStrings(actions, want)
		if i >= len(actions) || actions[i] != want {
			t.Errorf("audit log lacks %q: %v", want, actions)
		}
	}

	if r := e.post("/responders/logout", url.Values{"csrf": {csrf}}); r.status != http.StatusSeeOther {
		t.Fatalf("logout: %d", r.status)
	}
	public := e.get("/p/" + missing.PublicID)
	expect(t, public, http.StatusOK, "Contact responders")
	refute(t, public, "deceased", "Confirmed by hospital")
}

func TestAdminAccountsAndImport(t *testing.T) {
	e := setup(t, nil)
	csrf := login(t, e, "boss", store.RoleAdmin)
	r := e.post("/responders/users", url.Values{"csrf": {csrf}, "username": {"helper"}, "role": {"responder"}})
	expect(t, r, http.StatusOK, "New password for helper")
	expect(t, e.post("/responders/users", url.Values{"csrf": {csrf}, "username": {"helper"}}), http.StatusConflict)
	expect(t, e.post("/responders/users/boss", url.Values{"csrf": {csrf}, "action": {"disable"}}), http.StatusUnprocessableEntity)
	expect(t, e.post("/responders/tokens", url.Values{"csrf": {csrf}, "name": {"Partner"}, "scope": {"read"}}), http.StatusOK, "af_")
	expect(t, e.get("/responders/audit"), http.StatusOK, "user_create", "token_create")

	f, err := os.ReadFile("../../examples/person-finder-1.2.xml")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf", csrf)
	_ = mw.WriteField("visibility", "public")
	fw, _ := mw.CreateFormFile("file", "pf.xml")
	_, _ = fw.Write(f)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/responders/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", e.srv.URL)
	expect(t, e.do(req), http.StatusOK, "PFIF 1.2: 2 created")
	expect(t, e.get("/search?q=okafor"), http.StatusOK, "Daniel Okafor")
}

func TestSMSWebhooks(t *testing.T) {
	e := setup(t, nil)
	post := func(path string, body string, headers map[string]string) resp {
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return e.do(req)
	}
	form := url.Values{"from": {"+15550111"}, "body": {"SAFE Pedro Ramirez & Lucia Ramirez, stadium"}}
	expect(t, post("/sms/inbound", form.Encode(), nil), http.StatusUnauthorized)
	r := post("/sms/inbound", form.Encode(), map[string]string{"Authorization": "Bearer webhook-secret"})
	expect(t, r, http.StatusOK, "Pedro Ramirez, Lucia Ramirez")
	var out map[string]string
	if err := json.Unmarshal([]byte(r.body), &out); err != nil || out["reply"] == "" {
		t.Fatalf("bad JSON reply: %s", r.body)
	}
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/sms/inbound", strings.NewReader(`{"phone":"+15550112","text":"FIND pedro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Secret", "webhook-secret")
	req.Header.Set("Accept", "text/plain")
	expect(t, e.do(req), http.StatusOK, "Pedro Ramirez: SAFE")

	// Twilio: only correctly signed requests are answered.
	tw := url.Values{"From": {"+15550120"}, "Body": {"SEGURO Tomás Ruiz"}, "To": {"+15550100"}}
	data := e.srv.URL + "/sms/twilio" + "BodySEGURO Tomás Ruiz" + "From+15550120" + "To+15550100"
	mac := hmac.New(sha1.New, []byte("twilio-token"))
	mac.Write([]byte(data))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	expect(t, post("/sms/twilio", tw.Encode(), map[string]string{"X-Twilio-Signature": "bad"}), http.StatusForbidden)
	r = post("/sms/twilio", tw.Encode(), map[string]string{"X-Twilio-Signature": sig})
	expect(t, r, http.StatusOK, "<Response><Message>", "A salvo: Tomás Ruiz")

	// Without configuration the webhooks do not exist.
	e2 := setup(t, map[string]string{"SMS_WEBHOOK_SECRET": "", "TWILIO_AUTH_TOKEN": ""})
	req, _ = http.NewRequest(http.MethodPost, e2.srv.URL+"/sms/inbound", strings.NewReader("from=1&body=x"))
	expect(t, e2.do(req), http.StatusNotFound)
}

func TestPFIFAPI(t *testing.T) {
	e := setup(t, nil)
	ctx := context.Background()
	p := &store.Person{FullName: "Api Person", Visibility: store.Limited, Origin: store.OriginWeb, AuthorPhone: "+15550123"}
	if err := e.st.CreatePerson(ctx, p, []store.Note{{Status: store.StatusIsNoteAuthor, Text: "limited message", Origin: store.OriginWeb}}, ""); err != nil {
		t.Fatal(err)
	}
	_, read, _ := e.st.CreateAPIToken(ctx, "reader", "read")
	_, write, _ := e.st.CreateAPIToken(ctx, "writer", "readwrite")
	call := func(method, path, token string, body io.Reader) resp {
		req, _ := http.NewRequest(method, e.srv.URL+path, body)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return e.do(req)
	}
	expect(t, call(http.MethodGet, "/api/v1/pfif", "", nil), http.StatusUnauthorized)
	expect(t, call(http.MethodGet, "/api/v1/pfif?since=yesterday", read, nil), http.StatusBadRequest)
	expect(t, call(http.MethodGet, "/api/v1/pfif?since=2020-01-01T00:00:00Z", read, nil), http.StatusOK, "Api Person", "+15550123", "limited message")
	expect(t, call(http.MethodPost, "/api/v1/pfif", read, strings.NewReader("<x/>")), http.StatusForbidden)
	expect(t, call(http.MethodPost, "/api/v1/pfif", write, strings.NewReader("<x/>")), http.StatusBadRequest)
	doc := `<pfif:pfif xmlns:pfif="http://zesty.ca/pfif/1.4"><pfif:person>
<pfif:person_record_id>partner.org/person.9</pfif:person_record_id><pfif:full_name>Partner Person</pfif:full_name>
</pfif:person></pfif:pfif>`
	expect(t, call(http.MethodPost, "/api/v1/pfif?visibility=public", write, strings.NewReader(doc)), http.StatusOK, `"created": 1`)

	// The public feed shows names and statuses, never contacts or limited
	// messages.
	feed := e.get("/pfif.xml")
	expect(t, feed, http.StatusOK, "Api Person", "Partner Person", "is_note_author")
	refute(t, feed, "+15550123", "limited message")

	e2 := setup(t, map[string]string{"PUBLIC_FEED": "false"})
	expect(t, e2.get("/pfif.xml"), http.StatusNotFound)
}

func TestRateLimit(t *testing.T) {
	e := setup(t, map[string]string{"RATE_LIMIT_POSTS_PER_HOUR": "4"})
	codes := map[int]int{}
	for i := 0; i < 12; i++ {
		codes[e.post("/safe", url.Values{"name": {"Person"}}).status]++
	}
	if codes[http.StatusSeeOther] != 10 || codes[http.StatusTooManyRequests] != 2 {
		t.Errorf("status counts: %v", codes)
	}
}

func TestOperationalEndpoints(t *testing.T) {
	e := setup(t, nil)
	expect(t, e.get("/healthz"), http.StatusOK, `"status": "ok"`)
	expect(t, e.get("/version"), http.StatusOK, `"version"`, `"build"`)
	expect(t, e.get("/robots.txt"), http.StatusOK, "Disallow: /p/", "Disallow: /search")
	r := e.get("/favicon.ico")
	if r.status != http.StatusOK || r.header.Get("Content-Type") != "image/svg+xml" {
		t.Errorf("favicon: %d %s", r.status, r.header.Get("Content-Type"))
	}
	expect(t, e.get("/does-not-exist"), http.StatusNotFound, "Page not found")
	if !strings.Contains(e.get("/p/aaaaaaaaaa").body, "noindex") {
		t.Error("person pages must be noindex")
	}
}

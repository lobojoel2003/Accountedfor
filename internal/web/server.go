// Package web serves Accountedfor's HTML pages, SMS webhooks and PFIF API.
//
// Pages are rendered on the server with no JavaScript, web fonts or images
// so that the board works on old phones and congested networks.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/config"
	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/sms"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/version"
)

const sessionCookie = "af_session"

// Server is the HTTP application.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	log    *slog.Logger
	render *renderer
	sms    *sms.Service

	postLimit  *limiter
	loginLimit *limiter
	smsLimit   *limiter
}

// New builds a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) (*Server, error) {
	r, err := newRenderer(cfg.Location)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:        cfg,
		store:      st,
		log:        log,
		render:     r,
		postLimit:  newLimiter(cfg.PostsPerHourPerIP, max(cfg.PostsPerHourPerIP/4, 10)),
		loginLimit: newLimiter(40, 10),
		smsLimit:   newLimiter(cfg.SMSPerHourPerPhone, max(cfg.SMSPerHourPerPhone/3, 5)),
	}
	s.sms = &sms.Service{
		Store:             st,
		SiteName:          cfg.SiteName,
		BaseURL:           strings.TrimRight(cfg.BaseURL.String(), "/"),
		DefaultVisibility: store.ParseVisibility(cfg.SMSDefaultVisibility, store.Limited),
		Retention:         cfg.Retention(),
		Allow:             func(phone string) bool { return s.smsLimit.allow(phone) },
	}
	return s, nil
}

// Handler returns the root HTTP handler with all middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public pages.
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /safe", s.safeForm)
	mux.HandleFunc("POST /safe", s.safeSubmit)
	mux.HandleFunc("GET /missing", s.missingForm)
	mux.HandleFunc("POST /missing", s.missingSubmit)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /p/{id}", s.person)
	mux.HandleFunc("POST /p/{id}/notes", s.addNote)
	mux.HandleFunc("POST /p/{id}/me", s.thatsMe)
	mux.HandleFunc("GET /p/{id}/posted", s.posted)
	mux.HandleFunc("GET /p/{id}/edit", s.editForm)
	mux.HandleFunc("POST /p/{id}/edit", s.editSubmit)
	mux.HandleFunc("GET /help", s.static("help", "help.title"))
	mux.HandleFunc("GET /privacy", s.privacy)

	// Operations.
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /version", s.versionInfo)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /favicon.ico", favicon)

	// Machine interfaces.
	mux.HandleFunc("GET /pfif.xml", s.publicFeed)
	mux.HandleFunc("GET /api/v1/pfif", s.apiExport)
	mux.HandleFunc("POST /api/v1/pfif", s.apiImport)
	mux.HandleFunc("POST /sms/twilio", s.smsTwilio)
	mux.HandleFunc("POST /sms/inbound", s.smsInbound)

	// Responders.
	mux.HandleFunc("GET /responders/login", s.loginForm)
	mux.HandleFunc("POST /responders/login", s.loginSubmit)
	mux.HandleFunc("POST /responders/logout", s.responder(s.logout))
	mux.HandleFunc("GET /responders", s.responder(s.dashboard))
	mux.HandleFunc("GET /responders/{$}", s.responder(s.dashboard))
	mux.HandleFunc("GET /responders/matches", s.responder(s.matches))
	mux.HandleFunc("POST /responders/matches", s.responder(s.matchAction))
	mux.HandleFunc("GET /responders/new", s.responder(s.newRecordForm))
	mux.HandleFunc("POST /responders/new", s.responder(s.newRecordSubmit))
	mux.HandleFunc("GET /responders/data", s.responder(s.dataPage))
	mux.HandleFunc("GET /responders/export.csv", s.responder(s.exportCSV))
	mux.HandleFunc("GET /responders/export.xml", s.responder(s.exportPFIF))
	mux.HandleFunc("POST /responders/import", s.admin(s.importPFIF))
	mux.HandleFunc("POST /responders/person/{id}/{action}", s.responder(s.moderatePerson))
	mux.HandleFunc("POST /responders/notes/{id}/{action}", s.responder(s.moderateNote))
	mux.HandleFunc("GET /responders/users", s.admin(s.users))
	mux.HandleFunc("POST /responders/users", s.admin(s.createUser))
	mux.HandleFunc("POST /responders/users/{username}", s.admin(s.userAction))
	mux.HandleFunc("POST /responders/tokens", s.admin(s.createToken))
	mux.HandleFunc("POST /responders/tokens/{id}/revoke", s.admin(s.revokeToken))
	mux.HandleFunc("GET /responders/audit", s.admin(s.auditLog))

	mux.HandleFunc("/", s.notFoundHandler)

	// Browsers send Sec-Fetch-Site/Origin on every form post; reject posts
	// that come from other sites. Webhooks and the API authenticate with
	// secrets instead.
	cop := http.NewCrossOriginProtection()
	if err := cop.AddTrustedOrigin(s.cfg.BaseURL.Scheme + "://" + s.cfg.BaseURL.Host); err != nil {
		s.log.Warn("BASE_URL is not a valid origin", "err", err)
	}
	cop.AddInsecureBypassPattern("POST /sms/")
	cop.AddInsecureBypassPattern("POST /api/")
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.fail(w, r, http.StatusForbidden, "error.forbidden_title", "error.forbidden_text")
	}))

	var h http.Handler = mux
	h = cop.Handler(h)
	h = s.withContext(h)
	h = gzipResponses(h)
	h = s.securityHeaders(h)
	h = s.logRequests(h)
	return http.MaxBytesHandler(h, 64<<20)
}

// page builds the common page data for a request.
func (s *Server) page(r *http.Request, title string) *Page {
	lang := langFrom(r)
	p := &Page{
		Lang:            lang,
		Site:            s.cfg.SiteName,
		Event:           s.cfg.EventName,
		Notice:          s.cfg.Notice,
		Operator:        s.cfg.OperatorName,
		OperatorContact: s.cfg.OperatorContact,
		SMSNumber:       s.cfg.SMSNumber,
		BaseURL:         strings.TrimRight(s.cfg.BaseURL.String(), "/"),
		Version:         version.Version,
		Build:           version.BuildNumber,
		NoIndex:         true,
	}
	if title != "" {
		p.Title = i18n.T(lang, title)
	}
	if sess := sessionFrom(r); sess != nil {
		p.User = &sess.User
		p.CSRF = sess.CSRFToken
	}
	for _, l := range i18n.Languages {
		q := url.Values{}
		for k, v := range r.URL.Query() {
			if k != "lang" && k != "key" {
				q[k] = v
			}
		}
		q.Set("lang", l.Code)
		p.LangLinks = append(p.LangLinks, langLink{
			Code: l.Code, Name: l.Name, Current: l.Code == lang,
			URL: r.URL.Path + "?" + q.Encode(),
		})
	}
	return p
}

func (s *Server) show(w http.ResponseWriter, r *http.Request, status int, name string, data pageData) {
	if err := s.render.render(w, status, name, data); err != nil {
		s.log.Error("render", "page", name, "err", err)
	}
}

type errorPage struct {
	*Page
	Heading, Message string
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	p := s.page(r, heading)
	s.show(w, r, status, "error", &errorPage{Page: p, Heading: heading, Message: message})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusNotFound, "error.not_found_title", "error.not_found_text")
}

func (s *Server) notFoundHandler(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) }

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.log.Error("request failed", "path", r.URL.Path, "err", err)
	s.fail(w, r, http.StatusInternalServerError, "error.server_title", "error.server_text")
}

func (s *Server) tooMany(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "300")
	s.fail(w, r, http.StatusTooManyRequests, "error.too_many_title", "error.too_many_text")
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) static(name, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := s.page(r, title)
		p.NoIndex = false
		s.show(w, r, http.StatusOK, name, p)
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		status, code = "database unavailable", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status, "version": version.Version, "build": version.BuildNumber})
}

func (s *Server) versionInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.Get())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// Names of people in a disaster must not end up in search engines.
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /p/\nDisallow: /search\nDisallow: /responders\nDisallow: /api/\nDisallow: /pfif.xml\nDisallow: /sms/\n"))
}

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	_, _ = w.Write([]byte(faviconSVG))
}

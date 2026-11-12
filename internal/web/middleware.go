package web

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/i18n"
	"github.com/lobojoel2003/accountedfor/internal/store"
)

type ctxKey int

const (
	ctxLang ctxKey = iota
	ctxSession
)

func langFrom(r *http.Request) string {
	if l, ok := r.Context().Value(ctxLang).(string); ok {
		return l
	}
	return "en"
}

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

func userFrom(r *http.Request) *store.User {
	if s := sessionFrom(r); s != nil {
		return &s.User
	}
	return nil
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests logs one line per request. Query strings are never logged:
// they can contain names and private edit keys.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				if rec.status == 0 {
					http.Error(rec, "internal error", http.StatusInternalServerError)
				}
			}
			level := slog.LevelInfo
			if r.URL.Path == "/healthz" {
				level = slog.LevelDebug
			}
			s.log.Log(r.Context(), level, "request",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

// securityHeaders sets a strict Content Security Policy and related headers.
// Pages contain no scripts at all; the only inline style is allowed by hash.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	csp := "default-src 'none'; style-src '" + s.render.cssHash + "'; img-src 'self' data:; " +
		"form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	hsts := s.cfg.BaseURL.Scheme == "https"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// Shelters share devices: never let a browser or proxy keep a copy of
		// a page with names on it.
		h.Set("Cache-Control", "no-store")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestCompression)
	return w
}}

type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

func compressible(contentType string) bool {
	for _, t := range []string{"text/", "application/xml", "application/json", "image/svg+xml"} {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	if code != http.StatusNoContent && code != http.StatusNotModified &&
		h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type")) {
		g.compress = true
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		g.gz = gzipPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.compress {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Flush() {
	if g.compress {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) close() {
	if g.compress {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
	}
}

// gzipResponses compresses text responses. A typical page shrinks from
// about 9 KB to about 3 KB, which matters more than anything else on a
// congested network.
func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// withContext resolves the language and the responder session.
func (s *Server) withContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := ""
		if l := r.URL.Query().Get("lang"); i18n.Supported(l) {
			lang = l
			http.SetCookie(w, &http.Cookie{Name: "lang", Value: l, Path: "/", MaxAge: 365 * 24 * 3600,
				HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
		} else if c, err := r.Cookie("lang"); err == nil && i18n.Supported(c.Value) {
			lang = c.Value
		} else {
			lang = i18n.Match(r.Header.Get("Accept-Language"), s.cfg.DefaultLanguage)
		}
		ctx := context.WithValue(r.Context(), ctxLang, lang)

		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			sess, err := s.store.GetSession(ctx, c.Value)
			if err == nil {
				ctx = context.WithValue(ctx, ctxSession, sess)
			} else {
				s.clearSessionCookie(w)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIP returns the address used for rate limiting.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				if ip := strings.TrimSpace(parts[i]); ip != "" {
					return ip
				}
			}
		}
		if ip := r.Header.Get("X-Real-Ip"); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter is a token-bucket rate limiter keyed by an arbitrary string.
type limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity float64
	perSec   float64
	lastGC   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// newLimiter allows burst events at once and perHour events per hour on
// average. A perHour of 0 disables limiting.
func newLimiter(perHour, burst int) *limiter {
	return &limiter{
		buckets:  map[string]*bucket{},
		capacity: float64(burst),
		perSec:   float64(perHour) / 3600,
		lastGC:   time.Now(),
	}
}

func (l *limiter) allow(key string) bool {
	if l == nil || l.perSec == 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 2*time.Hour {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.capacity, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.perSec
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

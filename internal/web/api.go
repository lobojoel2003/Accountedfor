package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/exchange"
	"github.com/lobojoel2003/accountedfor/internal/sms"
	"github.com/lobojoel2003/accountedfor/internal/store"
)

func parseSince(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("since must be an RFC 3339 time such as 2026-01-02T15:04:05Z")
	}
	return t, nil
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// publicFeed serves the records anyone may see as PFIF 1.4. Contact details
// and private records are never included.
func (s *Server) publicFeed(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.PublicFeed {
		s.notFound(w, r)
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	if _, err := exchange.Export(r.Context(), s.store, w, exchange.ExportOptions{
		Since: since, Level: exchange.Public, Tombstones: true,
	}); err != nil {
		s.log.Error("public feed", "err", err)
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if v, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (s *Server) apiToken(w http.ResponseWriter, r *http.Request) (*store.APIToken, bool) {
	t, err := s.store.CheckAPIToken(r.Context(), bearer(r))
	if errors.Is(err, store.ErrNotFound) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="accountedfor"`)
		apiError(w, http.StatusUnauthorized, "a valid API token is required (Authorization: Bearer af_...)")
		return nil, false
	}
	if err != nil {
		s.log.Error("api token", "err", err)
		apiError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	return t, true
}

// apiExport serves full PFIF records to trusted partners.
func (s *Server) apiExport(w http.ResponseWriter, r *http.Request) {
	t, ok := s.apiToken(w, r)
	if !ok {
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	n, err := exchange.Export(r.Context(), s.store, w, exchange.ExportOptions{
		Since: since, Level: exchange.Full, Tombstones: true,
	})
	if err != nil {
		s.log.Error("api export", "err", err)
		return
	}
	_ = s.store.Audit(r.Context(), "token:"+t.Name, "api_export", fmt.Sprintf("%d records since %s", n, r.URL.Query().Get("since")))
}

// apiImport accepts a PFIF document from a partner with a readwrite token.
func (s *Server) apiImport(w http.ResponseWriter, r *http.Request) {
	t, ok := s.apiToken(w, r)
	if !ok {
		return
	}
	if !t.CanWrite() {
		apiError(w, http.StatusForbidden, "this token is read-only")
		return
	}
	res, err := exchange.Import(r.Context(), s.store, http.MaxBytesReader(w, r.Body, maxImport), exchange.ImportOptions{
		Visibility: store.ParseVisibility(r.URL.Query().Get("visibility"), store.Limited),
		Retention:  s.cfg.Retention(),
	})
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.store.Audit(r.Context(), "token:"+t.Name, "api_import", res.Summary())
	writeJSON(w, http.StatusOK, map[string]any{
		"version": res.Version, "created": res.Created, "updated": res.Updated, "deleted": res.Deleted,
		"notes": res.Notes, "skipped": res.Skipped, "problems": res.Problems,
	})
}

// smsTwilio is the webhook for Twilio Programmable Messaging. Configure the
// number's "A message comes in" webhook as POST {BASE_URL}/sms/twilio.
func (s *Server) smsTwilio(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TwilioAuthToken == "" {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	fullURL := s.cfg.URL(r.URL.RequestURI())
	if !sms.ValidTwilioSignature(s.cfg.TwilioAuthToken, fullURL, r.PostForm, r.Header.Get("X-Twilio-Signature")) {
		s.log.Warn("rejected SMS webhook with bad Twilio signature", "url", s.cfg.URL(r.URL.Path))
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	reply, err := s.sms.Handle(r.Context(), r.PostForm.Get("From"), r.PostForm.Get("Body"))
	if err != nil {
		s.log.Error("sms", "err", err)
		reply = "Sorry, something went wrong. Please try again in a few minutes."
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = io.WriteString(w, sms.TwiML(reply))
}

type inboundSMS struct {
	From    string `json:"from"`
	Phone   string `json:"phone"`
	Sender  string `json:"sender"`
	Body    string `json:"body"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// smsInbound is a generic webhook for other SMS gateways (Android gateway
// apps, Africa's Talking, Vonage, RapidPro, ...). It accepts form or JSON
// bodies with "from" and "body" (or "text"/"message") and answers with
// {"reply": "..."}, or plain text when the gateway asks for text/plain.
func (s *Server) smsInbound(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.SMSWebhookSecret
	if secret == "" {
		s.notFound(w, r)
		return
	}
	given := firstNonEmpty(bearer(r), r.Header.Get("X-Webhook-Secret"), r.URL.Query().Get("secret"))
	if subtle.ConstantTimeCompare([]byte(given), []byte(secret)) != 1 {
		apiError(w, http.StatusUnauthorized, "invalid webhook secret")
		return
	}
	var msg inboundSMS
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "application/json" {
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&msg); err != nil {
			apiError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			apiError(w, http.StatusBadRequest, "invalid form")
			return
		}
		f := r.PostForm
		msg = inboundSMS{
			From: firstNonEmpty(f.Get("from"), f.Get("From"), f.Get("phone"), f.Get("sender"), f.Get("msisdn")),
			Body: firstNonEmpty(f.Get("body"), f.Get("Body"), f.Get("text"), f.Get("message")),
		}
	}
	from := firstNonEmpty(msg.From, msg.Phone, msg.Sender)
	body := firstNonEmpty(msg.Body, msg.Text, msg.Message)
	if from == "" {
		apiError(w, http.StatusBadRequest, "missing sender phone number (from)")
		return
	}
	reply, err := s.sms.Handle(r.Context(), from, body)
	if err != nil {
		s.log.Error("sms", "err", err)
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/plain") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, reply)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": reply})
}

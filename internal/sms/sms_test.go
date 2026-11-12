package sms

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/lobojoel2003/accountedfor/internal/store"
)

func TestParse(t *testing.T) {
	tests := []struct {
		body string
		want Command
	}{
		{"SAFE Maria Lopez", Command{Kind: Safe, Lang: "en", Names: []string{"Maria Lopez"}}},
		{"safe maria lopez, at the school shelter", Command{Kind: Safe, Lang: "en", Names: []string{"maria lopez"}, Message: "at the school shelter"}},
		{"I’m safe Tomás Ruiz: Grace Church", Command{Kind: Safe, Lang: "en", Names: []string{"Tomás Ruiz"}, Message: "Grace Church"}},
		{"SAFE Maria Lopez & Juan Lopez + Ana, stadium", Command{Kind: Safe, Lang: "en", Names: []string{"Maria Lopez", "Juan Lopez", "Ana"}, Message: "stadium"}},
		{"SAFE PRIVATE Rosa Díaz", Command{Kind: Safe, Lang: "en", Names: []string{"Rosa Díaz"}, Visibility: store.Private}},
		{"Seguro público Rosa Díaz", Command{Kind: Safe, Lang: "es", Names: []string{"Rosa Díaz"}, Visibility: store.Public}},
		{"estoy a salvo Luis", Command{Kind: Safe, Lang: "es", Names: []string{"Luis"}}},
		{"SAFE\nMaria\nat home", Command{Kind: Safe, Lang: "en", Names: []string{"Maria"}, Message: "at home"}},
		{"FIND maria lopez", Command{Kind: Find, Lang: "en", Names: []string{"maria lopez"}}},
		{"buscar José & María", Command{Kind: Find, Lang: "es", Names: []string{"José & María"}}},
		{"MISSING Carlos Vega - last seen at the bridge", Command{Kind: Missing, Lang: "en", Names: []string{"Carlos Vega"}, Message: "last seen at the bridge"}},
		{"desaparecida Ana", Command{Kind: Missing, Lang: "es", Names: []string{"Ana"}}},
		{"delete", Command{Kind: Delete, Lang: "en"}},
		{"BORRAR", Command{Kind: Delete, Lang: "es"}},
		{"STOP", Command{Kind: Stop, Lang: "en"}},
		{"hola necesito ayuda", Command{Kind: Help, Lang: "es"}},
		{"hello?", Command{Kind: Help, Lang: "en"}},
		{"", Command{Kind: Help, Lang: "en"}},
		// A keyword must be a whole word.
		{"SAFETY first", Command{Kind: Help, Lang: "en"}},
		{"Findlay Smith", Command{Kind: Help, Lang: "en"}},
	}
	for _, tt := range tests {
		got := Parse(tt.body)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q)\n got  %+v\n want %+v", tt.body, got, tt.want)
		}
	}
}

func TestValidTwilioSignature(t *testing.T) {
	const token = "12345"
	const u = "https://mycompany.com/myapp.php?foo=1&bar=2"
	form := url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+12349013030"},
		"Digits":  {"1234"},
		"From":    {"+12349013030"},
		"To":      {"+18005551212"},
	}
	// Known-good value from Twilio's documentation.
	const docSig = "0/KCTR6DLpKmkAf8muzZqo1nDgQ="
	if !ValidTwilioSignature(token, u, form, docSig) {
		t.Fatal("documented signature rejected")
	}
	if ValidTwilioSignature(token, u, form, "bogus") || ValidTwilioSignature("", u, form, docSig) {
		t.Fatal("bad signature accepted")
	}
	form.Set("Digits", "9999")
	if ValidTwilioSignature(token, u, form, docSig) {
		t.Fatal("tampered form accepted")
	}

	// Our own computation for a typical SMS webhook.
	sms := url.Values{"From": {"+15550100"}, "Body": {"SAFE Maria"}}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte("https://x.org/sms/twilio" + "BodySAFE Maria" + "From+15550100"))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !ValidTwilioSignature(token, "https://x.org/sms/twilio", sms, sig) {
		t.Fatal("computed signature rejected")
	}
}

func TestTwiML(t *testing.T) {
	got := TwiML(`Safe: "A & B" <ok>`)
	if !strings.Contains(got, "<Message>Safe: &quot;A &amp; B&quot; &lt;ok&gt;</Message>") {
		t.Errorf("TwiML = %s", got)
	}
	if TwiML("") != `<?xml version="1.0" encoding="UTF-8"?><Response/>` {
		t.Error("empty reply must produce an empty response")
	}
}

func TestLimit(t *testing.T) {
	long := strings.Repeat("a", 1000)
	if got := []rune(limit(long)); len(got) != MaxReply {
		t.Errorf("limit gave %d runes", len(got))
	}
}

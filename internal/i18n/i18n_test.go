package i18n

import (
	"strings"
	"testing"
)

func TestCatalogsComplete(t *testing.T) {
	for _, lang := range Languages {
		cat := Catalog(lang.Code)
		if cat == nil {
			t.Fatalf("no catalog for %s", lang.Code)
		}
		for _, key := range Keys() {
			v, ok := cat[key]
			if !ok {
				t.Errorf("%s: missing key %q", lang.Code, key)
				continue
			}
			if strings.Count(v, "%") != strings.Count(en[key], "%") {
				t.Errorf("%s: %q has different format verbs than English", lang.Code, key)
			}
		}
		for key := range cat {
			if _, ok := en[key]; !ok {
				t.Errorf("%s: key %q is not in the English catalog", lang.Code, key)
			}
		}
	}
}

func TestPlural(t *testing.T) {
	if got := Tn("en", "search.count", 1); got != "1 person found" {
		t.Errorf("singular: %q", got)
	}
	if got := Tn("es", "search.count", 3); got != "3 personas encontradas" {
		t.Errorf("plural: %q", got)
	}
}

func TestPluralWithoutPlaceholder(t *testing.T) {
	for _, lang := range Languages {
		for _, n := range []int{1, 2} {
			if got := Tn(lang.Code, "posted.safe_text", n); strings.Contains(got, "%!") {
				t.Errorf("%s n=%d: %q", lang.Code, n, got)
			}
		}
	}
}

func TestFallback(t *testing.T) {
	if got := T("xx", "nav.help"); got != "How it works" {
		t.Errorf("unknown language should fall back to English, got %q", got)
	}
	if got := T("en", "no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key should return itself, got %q", got)
	}
}

func TestMatch(t *testing.T) {
	tests := []struct{ header, want string }{
		{"es-MX,es;q=0.9,en;q=0.8", "es"},
		{"en-US,en;q=0.9", "en"},
		{"fr-FR,fr;q=0.9,es;q=0.5", "es"},
		{"de", "en"},
		{"", "en"},
		{"en;q=0.2, es;q=0.8", "es"},
	}
	for _, tt := range tests {
		if got := Match(tt.header, "en"); got != tt.want {
			t.Errorf("Match(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
	if got := Match("de", "es"); got != "es" {
		t.Errorf("fallback not used: %q", got)
	}
}

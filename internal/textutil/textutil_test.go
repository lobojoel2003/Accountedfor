package textutil

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"José  O'Brien-Núñez":  "jose o brien nunez",
		"  MARÍA   lópez ":     "maria lopez",
		"Đặng Thị Ngọc":        "dang thi ngoc",
		"Søren Kierkegaard":    "soren kierkegaard",
		"Straße":               "strasse",
		"李小龍":                  "李小龍",
		"محمد علي":             "محمد علي",
		"!!!":                  "",
		"Anne-Marie\tDupont\n": "anne marie dupont",
	}
	for in, want := range tests {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("  hello\n\tworld\x00 ", 0); got != "hello world" {
		t.Errorf("Line = %q", got)
	}
	if got := Line("abcdef", 3); got != "abc" {
		t.Errorf("Line truncation = %q", got)
	}
	if got := Line("ñandú", 3); got != "ñan" {
		t.Errorf("Line truncates by rune, got %q", got)
	}
}

func TestMultiline(t *testing.T) {
	got := Multiline("\r\n\r\nfirst line\r\n\r\n\r\n\r\nsecond\x07 line\n\n", 0)
	if got != "first line\n\nsecond line" {
		t.Errorf("Multiline = %q", got)
	}
}

func TestPhones(t *testing.T) {
	if got := NormalizePhone(" +1 (555) 010-0199 "); got != "+15550100199" {
		t.Errorf("NormalizePhone = %q", got)
	}
	if got := NormalizePhone("555+123"); got != "555123" {
		t.Errorf("plus only allowed first, got %q", got)
	}
	if got := MaskPhone("+15550100199"); !strings.HasSuffix(got, "0199") || strings.Contains(got, "555") {
		t.Errorf("MaskPhone = %q", got)
	}
}

func TestSplitNames(t *testing.T) {
	got := SplitNames("Maria Lopez & Juan Lopez\n\n + Ana López; maria  lopez ;", 10, 50)
	want := []string{"Maria Lopez", "Juan Lopez", "Ana López"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitNames = %q, want %q", got, want)
	}
	if got := SplitNames("a\nb\nc\nd", 2, 50); len(got) != 2 {
		t.Errorf("max names not applied: %q", got)
	}
}

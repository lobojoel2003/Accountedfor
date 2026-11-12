// Package textutil cleans user input and normalizes names and phone numbers
// so that searches match regardless of accents, case or punctuation.
package textutil

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Line cleans a single-line field: control characters and line breaks become
// spaces, runs of whitespace collapse, and the result is cut to max runes.
func Line(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	return Truncate(strings.Join(strings.Fields(s), " "), max)
}

// Multiline cleans a free-text field, keeping line breaks but removing other
// control characters and collapsing more than one blank line.
func Multiline(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = Line(l, max)
		if l == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return Truncate(strings.TrimSpace(strings.Join(out, "\n")), max)
}

// Truncate cuts s to at most max runes. A max of 0 or less means no limit.
func Truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max]))
}

var foldReplacer = strings.NewReplacer(
	"ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "ð", "d",
	"þ", "th", "ı", "i", "ŀ", "l",
)

// NormalizeName turns a name into its search key: lower case, accents
// removed, punctuation replaced by spaces and whitespace collapsed.
// "José  O'Brien-Núñez" becomes "jose o brien nunez".
func NormalizeName(s string) string {
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(t, s)
	if err != nil {
		folded = s
	}
	folded = foldReplacer.Replace(strings.ToLower(folded))
	folded = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mc, r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, folded)
	return strings.Join(strings.Fields(folded), " ")
}

// NormalizePhone keeps the digits of a phone number and a leading "+".
func NormalizePhone(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MaskPhone hides all but the last 4 digits of a phone number.
func MaskPhone(s string) string {
	p := NormalizePhone(s)
	if len(p) <= 4 {
		return p
	}
	return strings.Repeat("•", 3) + p[len(p)-4:]
}

// SplitNames splits a list of names separated by line breaks, "&", "+" or
// ";" into individual, cleaned names. Empty entries are dropped and
// duplicates (by normalized form) removed.
func SplitNames(s string, maxNames, maxLen int) []string {
	f := func(r rune) bool {
		return r == '\n' || r == '\r' || r == '&' || r == '+' || r == ';'
	}
	seen := map[string]bool{}
	var names []string
	for _, part := range strings.FieldsFunc(s, f) {
		name := Line(part, maxLen)
		key := NormalizeName(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		if maxNames > 0 && len(names) == maxNames {
			break
		}
	}
	return names
}

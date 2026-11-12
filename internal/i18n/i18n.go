// Package i18n holds the user-interface translations.
//
// To add a language, add a map to catalogs (see en.go for every key) and an
// entry to Languages. Missing keys fall back to English.
package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Language is a supported UI language.
type Language struct {
	Code string
	// Name is the language's name in itself, e.g. "Español".
	Name string
}

// Languages lists the supported languages in the order shown to users.
var Languages = []Language{
	{Code: "en", Name: "English"},
	{Code: "es", Name: "Español"},
}

var catalogs = map[string]map[string]string{
	"en": en,
	"es": es,
}

// Supported reports whether code is a supported language.
func Supported(code string) bool {
	_, ok := catalogs[code]
	return ok
}

// T returns the translation of key in lang, falling back to English and
// finally to the key itself.
func T(lang, key string) string {
	if s, ok := catalogs[lang][key]; ok {
		return s
	}
	if s, ok := en[key]; ok {
		return s
	}
	return key
}

// Tf formats the translation of key with args.
func Tf(lang, key string, args ...any) string {
	return fmt.Sprintf(T(lang, key), args...)
}

// Tn picks the singular (key.one) or plural (key.other) form for n and
// formats n into it when the text has a placeholder ("You're safe" needs
// none, "%d people found" does).
func Tn(lang, key string, n int) string {
	form := ".other"
	if n == 1 {
		form = ".one"
	}
	s := T(lang, key+form)
	if !strings.Contains(s, "%") {
		return s
	}
	return fmt.Sprintf(s, n)
}

// Keys returns every key of the English catalog, sorted. Tests use it to
// check that translations are complete.
func Keys() []string {
	keys := make([]string, 0, len(en))
	for k := range en {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Catalog returns the raw map for lang, for tests.
func Catalog(lang string) map[string]string { return catalogs[lang] }

// Match picks the best supported language for an Accept-Language header.
func Match(acceptLanguage, fallback string) string {
	type candidate struct {
		code string
		q    float64
	}
	var cands []candidate
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if Supported(base) && q > 0 {
			cands = append(cands, candidate{base, q})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].q > cands[j].q })
	if len(cands) > 0 {
		return cands[0].code
	}
	if Supported(fallback) {
		return fallback
	}
	return "en"
}

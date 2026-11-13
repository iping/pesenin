// Package text normalises slugs, WhatsApp numbers, and rupiah for Pesenin.
package text

import (
	"fmt"
	"strings"
	"unicode"
)

var reservedBrand = map[string]struct{}{
	"app": {}, "api": {}, "admin": {}, "o": {}, "q": {}, "privasi": {}, "syarat": {},
	"login": {}, "help": {}, "bantuan": {}, "blog": {}, "static": {}, "id": {}, "en": {},
	"pesenin": {}, "masuk": {}, "auth": {}, "media": {}, "healthz": {}, "sitemap": {},
	"robots": {}, "sw": {}, "manifest": {}, "brand": {}, "akun": {}, "koin": {},
}

// Slug turns "Dapur Senja!" into "dapur-senja".
func Slug(in string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(in)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 30 {
		out = strings.Trim(out[:30], "-")
	}
	return out
}

// SlugOK reports whether a slug may be stored.
func SlugOK(slug string, store bool) (bool, string) {
	if slug != Slug(slug) || len(slug) < 3 || len(slug) > 30 || strings.Contains(slug, "--") {
		return false, "Pakai huruf kecil, angka, dan strip. 3 sampai 30 karakter."
	}
	if _, bad := reservedBrand[slug]; bad {
		return false, "Nama ini tidak bisa dipakai."
	}
	if store && slug == "po" {
		return false, "Nama ini tidak bisa dipakai."
	}
	return true, ""
}

// Suggest returns up to three free alternatives.
func Suggest(base string, taken func(string) bool) []string {
	var out []string
	for i := 2; i <= 20 && len(out) < 3; i++ {
		s := fmt.Sprintf("%s-%d", trimSlug(base, 27), i)
		s = Slug(s)
		if taken(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func trimSlug(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.Trim(s[:n], "-")
}

// WhatsApp normalises Indonesian numbers to E.164 (+62...).
func WhatsApp(in string) (string, error) {
	var d strings.Builder
	for _, r := range in {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	s := d.String()
	switch {
	case strings.HasPrefix(s, "0"):
		s = "62" + s[1:]
	case strings.HasPrefix(s, "8"):
		s = "62" + s
	}
	if !strings.HasPrefix(s, "62") || len(s) < 10 || len(s) > 15 {
		return "", fmt.Errorf("nomor WhatsApp tidak valid")
	}
	return "+" + s, nil
}

// WAForLink strips the plus for wa.me.
func WAForLink(e164 string) string {
	return strings.TrimPrefix(e164, "+")
}

// Rupiah groups with dots: 75137 -> 75.137.
func Rupiah(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// Fill replaces {name} placeholders in a WhatsApp template.
func Fill(body string, vars map[string]string) string {
	for k, v := range vars {
		body = strings.ReplaceAll(body, "{"+k+"}", v)
	}
	return body
}

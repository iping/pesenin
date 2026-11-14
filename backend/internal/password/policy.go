package password

import "unicode/utf8"

const (
	MinLen = 8
	MaxLen = 128
)

// Common passwords are rejected as a whole string, case-insensitively.
// There is no composition rule.
var common = map[string]struct{}{
	"password": {}, "password1": {}, "password123": {}, "passw0rd": {},
	"12345678": {}, "123456789": {}, "1234567890": {},
	"qwerty123": {}, "qwertyui": {}, "qwertyuiop": {}, "iloveyou": {},
	"admin123": {}, "letmein1": {}, "welcome1": {}, "abc12345": {},
	"11111111": {}, "00000000": {}, "87654321": {},
	"rahasia1": {}, "sayangku": {}, "indonesia": {}, "indonesia1": {},
	"pesenin1": {}, "ganteng123": {}, "cantik123": {}, "jakarta1": {},
}

// PolicyError is a refusal the UI can show. It never includes the password.
type PolicyError struct{ Message string }

func (e *PolicyError) Error() string { return e.Message }

// Check rejects short, huge, and very common passwords.
func Check(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinLen {
		return &PolicyError{Message: "Kata sandi minimal 8 karakter."}
	}
	if n > MaxLen || len(pw) > 1024 {
		return &PolicyError{Message: "Kata sandi kepanjangan. Maksimal 128 karakter."}
	}
	if _, ok := common[lower(pw)]; ok {
		return &PolicyError{Message: "Kata sandi ini terlalu umum. Pilih yang lain ya."}
	}
	return nil
}

func lower(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}

package auth

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

func NormalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if len(email) > 255 || len(email) < 4 {
		return "", ErrInvalidInput
	}
	for _, c := range email {
		if c > unicode.MaxASCII || unicode.IsSpace(c) || unicode.IsControl(c) {
			return "", ErrInvalidInput
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email {
		return "", ErrInvalidInput
	}
	return email, nil
}

func validName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 150 {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}

func validPassword(password string) bool {
	return len(password) <= 72 && utf8.RuneCountInString(password) >= 12 && utf8.ValidString(password)
}

func validCode(code string) bool {
	return len(code) == 6 && !strings.ContainsFunc(code, func(c rune) bool { return c < '0' || c > '9' })
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

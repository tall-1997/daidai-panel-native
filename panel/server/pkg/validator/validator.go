package validator

import (
	"regexp"
	"strings"
)

var usernameRegex = regexp.MustCompile(`^[\p{L}\p{N}_]{1,32}$`)

func ValidateUsername(username string) bool {
	return usernameRegex.MatchString(username)
}

func ValidatePassword(password string) bool {
	return len(password) >= 6 && len(password) <= 128
}

func SanitizeString(s string) string {
	return strings.TrimSpace(s)
}

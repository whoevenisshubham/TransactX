package common

import (
	"strings"
	"unicode/utf8"
)

func NormalizeIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func ValidLength(value string, min, max int) bool {
	length := utf8.RuneCountInString(value)
	return length >= min && length <= max
}

package state

import (
	"regexp"
	"strings"
)

func NormalizeJID(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return value
	}
	compact := regexp.MustCompile(`\D+`).ReplaceAllString(value, "")
	if compact == "" {
		return value
	}
	return compact + "@s.whatsapp.net"
}

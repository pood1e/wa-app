package wire

import (
	"strings"

	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func WaContactUsername(value string) string {
	value = wacore.WAContactName(value)
	if value == "" || strings.Contains(value, "@") || strings.Contains(value, "://") {
		return ""
	}
	return value
}

package shared

import (
	"strings"
	"unicode/utf8"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func RequireBusinessName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "business name must contain 1 to 256 characters", false)
	}
	return name, nil
}

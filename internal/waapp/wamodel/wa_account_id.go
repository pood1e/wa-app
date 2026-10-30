package wamodel

import (
	"errors"
	"regexp"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

var waAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)

// RequireWAAccountID validates and normalizes a caller-supplied WA account ID.
func RequireWAAccountID(value string) (string, error) {
	accountID := strings.TrimSpace(value)
	if accountID == "" {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id is required", false)
	}
	if !waAccountIDPattern.MatchString(accountID) {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id must use letters, digits, colon, underscore or dash", false)
	}
	return accountID, nil
}

// IsWAAccountNotFound reports whether err is a WA-account-not-found error.
func IsWAAccountNotFound(err error) bool {
	var appErr *shared.AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND
}

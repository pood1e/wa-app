package store

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestPersistedErrorFieldsPreserveAbsentAndRetryableErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     *waappv1.WaError
		code      string
		message   string
		retryable bool
	}{
		{name: "nil"},
		{name: "unspecified", input: &waappv1.WaError{Message: "ignored", Retryable: true}},
		{name: "rejected", input: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, Message: "rejected"}, code: "WA_ERROR_CODE_REJECTED", message: "rejected"},
		{name: "retryable", input: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, Message: "cooldown", Retryable: true}, code: "WA_ERROR_CODE_RATE_LIMITED", message: "cooldown", retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if errCode(test.input) != test.code || errMessage(test.input) != test.message || errRetryable(test.input) != test.retryable {
				t.Fatal("persisted error fields changed after removing error conversion")
			}
		})
	}
}

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestProfilePictureErrorsPreserveAbsenceAndTransientFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		err          error
		status       int
		cache, retry string
	}{
		{"not_found", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "missing", false), http.StatusNotFound, "private, max-age=30", ""},
		{"busy", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "busy", true), http.StatusServiceUnavailable, "no-store", "5"},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "no-store", "5"},
		{"upstream_failure", errors.New("upstream failed"), http.StatusBadGateway, "no-store", "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeProfilePictureError(response, tc.err)
			if response.Code != tc.status || response.Header().Get("Cache-Control") != tc.cache || response.Header().Get("Retry-After") != tc.retry {
				t.Fatalf("picture error: status=%d cache=%q retry=%q", response.Code, response.Header().Get("Cache-Control"), response.Header().Get("Retry-After"))
			}
		})
	}
}

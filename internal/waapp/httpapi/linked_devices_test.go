package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func TestLinkedDeviceHTTPRejectsInvalidMethodsAndPayloads(t *testing.T) {
	handler := &dashboardHTTP{service: account.New(nil, nil, nil, nil, nil, nil)}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodDelete, "/api/wa/linked-devices", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/wa/linked-devices/unlink", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/wa/linked-devices", `{"unknown_field":true}`, http.StatusBadRequest},
		{http.MethodPost, "/api/wa/linked-devices", `{"qr_code":"unexpected plain string"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/wa/linked-devices", `{"qr_code":{"value":"` + strings.Repeat("x", 17000) + `"}}`, http.StatusBadRequest},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.handleLinkedDevices(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != test.status {
				t.Fatalf("HTTP status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

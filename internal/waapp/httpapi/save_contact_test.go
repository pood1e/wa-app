package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func TestSaveContactHTTPRejectsInvalidInputBeforeResolving(t *testing.T) {
	handler := &dashboardHTTP{service: account.New(nil, nil, nil, nil, nil, nil)}
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"missing body", http.MethodPost, "", http.StatusBadRequest},
		{"unknown field", http.MethodPost, `{"jid":"100001@lid"}`, http.StatusBadRequest},
		{"oversized body", http.MethodPost, strings.Repeat(" ", 4097), http.StatusBadRequest},
		{"missing account", http.MethodPost, `{"phone_number":"+12025550124"}`, http.StatusOK},
		{"invalid phone", http.MethodPost, `{"wa_account_id":"waacc_fixture","phone_number":"+123"}`, http.StatusOK},
		{"unsupported method", http.MethodPut, `{}`, http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/api/wa/contacts", strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			handler.handleContacts(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if tc.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, POST" {
				t.Fatal("contact resource did not advertise the save method")
			}
			if tc.status == http.StatusOK {
				value := new(waappv1.SaveWAContactResponse)
				if err := protojson.Unmarshal(response.Body.Bytes(), value); err != nil || value.GetContact() != nil || value.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED {
					t.Fatal("invalid contact input did not produce a typed validation error")
				}
			}
		})
	}
}

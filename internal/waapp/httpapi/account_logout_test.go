package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

func TestAccountLogoutHTTP(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for i, id := range []string{"waacc_route", "waacc_other"} {
		if err := repo.SaveWAAccount(t.Context(), &waappv1.WAAccount{WaAccountId: id, DisplayName: id, Phone: &waappv1.PhoneTarget{E164Number: fmt.Sprintf("+1202555012%d", i)}, Status: waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE}); err != nil {
			t.Fatal(err)
		}
	}
	handler := &dashboardHTTP{service: account.New(repo, nil, nil, nil, nil, nil)}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/wa/accounts/waacc_other/logout", "{}", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/wa/accounts/waacc_other/logout", `{"unexpected_field":true}`, http.StatusBadRequest},
		{http.MethodPost, "/api/wa/accounts/waacc_route/logout", `{"wa_account_id":"waacc_other"}`, http.StatusOK},
		{http.MethodPost, "/api/wa/accounts/waacc_route/logout", `{}`, http.StatusOK},
	} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		result := httptest.NewRecorder()
		handler.handleAccount(result, request)
		if result.Code != tc.status {
			t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, result.Code, tc.status)
		}
		if tc.status == http.StatusOK {
			response := &waappv1.LogoutWAAccountResponse{}
			if err := protojson.Unmarshal(result.Body.Bytes(), response); err != nil || response.GetError() != nil || response.GetAccount().GetWaAccountId() != "waacc_route" || response.GetAccount().GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT {
				t.Fatal("logout did not honor the route or required a live protocol connection")
			}
		}
	}
	other, err := repo.GetWAAccount(t.Context(), "waacc_other")
	if err != nil || other.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE {
		t.Fatal("logout changed a different account")
	}
}

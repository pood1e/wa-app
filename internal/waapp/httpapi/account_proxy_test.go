package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestAccountProxyHTTP(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, id := range []string{"waacc_proxy_route", "waacc_proxy_other"} {
		if err := repo.SaveWAAccount(t.Context(), &waappv1.WAAccount{WaAccountId: id, Phone: &waappv1.PhoneTarget{E164Number: "+" + id}}); err != nil {
			t.Fatal(err)
		}
	}
	handler := &dashboardHTTP{service: account.New(repo, nil, nil, nil, nil, nil)}
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{http.MethodPost, `{}`, http.StatusMethodNotAllowed},
		{http.MethodPut, `{"unexpected":true}`, http.StatusBadRequest},
		{http.MethodPut, `{"wa_account_id":"waacc_proxy_other","proxy":{"protocol":"ACCOUNT_PROXY_PROTOCOL_HTTP","host":"proxy.example","port":8080,"username":"fixture-user","password":{"value":"fixture-password"}}}`, http.StatusOK},
		{http.MethodGet, ``, http.StatusOK},
	} {
		request := httptest.NewRequest(tc.method, "/api/wa/accounts/waacc_proxy_route/proxy", strings.NewReader(tc.body))
		response := httptest.NewRecorder()
		handler.handleAccount(response, request)
		if response.Code != tc.status {
			t.Fatalf("HTTP status = %d, want %d", response.Code, tc.status)
		}
		if tc.status == http.StatusOK {
			value := new(waappv1.AccountProxyResponse)
			if err := protojson.Unmarshal(response.Body.Bytes(), value); err != nil || value.GetError() != nil || value.GetProxy().GetPassword().GetValue() != "" || value.GetProxy().GetPassword().GetRedactedValue() == "" || strings.Contains(response.Body.String(), "fixture-password") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("proxy response exposed credentials or failed")
			}
		}
	}
	other, err := repo.GetAccountProxy(t.Context(), "waacc_proxy_other")
	if err != nil || other != nil {
		t.Fatal("request body changed proxy for a different account")
	}
}

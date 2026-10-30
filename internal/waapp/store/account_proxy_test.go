package store_test

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"google.golang.org/protobuf/proto"
)

func TestAccountProxy(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			t.Run("lifecycle", func(t *testing.T) { testAccountProxyLifecycle(t, backend) })
			t.Run("password_updates", func(t *testing.T) { testAccountProxyUpdates(t, backend) })
			t.Run("acceptance_rollback", func(t *testing.T) { testAccountProxyRollback(t, backend) })
		})
	}
}

func fixtureProxy() *waappv1.AccountProxy {
	return &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5, Host: "proxy.example", Port: 1080, Username: "fixture-user", Password: &waappv1.SensitiveText{Value: "fixture-password"}}
}

func acceptProxyAccount(t *testing.T, repo repository, material *waappv1.AcceptedAccountMaterial, requestID string) *waappv1.WAAccount {
	t.Helper()
	response, err := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: requestID}, Material: material})
	if err != nil || response.GetError() != nil || response.GetAccount() == nil {
		t.Fatal("proxy account acceptance failed")
	}
	return response.GetAccount()
}

func testAccountProxyLifecycle(t *testing.T, backend string) {
	repo, reopen := openRepository(t, backend)
	material := testMaterial(t)
	material.Proxy = fixtureProxy()
	accepted := acceptProxyAccount(t, repo, material, "proxy-initial")
	accountID := accepted.GetWaAccountId()
	application := account.New(repo, nil, nil, nil, nil, nil)
	response, err := application.GetAccountProxy(t.Context(), &waappv1.GetAccountProxyRequest{WaAccountId: accountID})
	if err != nil || response.GetError() != nil || response.GetProxy().GetPassword().GetValue() != "" || response.GetProxy().GetPassword().GetRedactedValue() == "" {
		t.Fatal("public proxy read exposed a password or omitted its presence")
	}
	resolved, err := application.ResolveAccountAddProxy(t.Context(), &waappv1.ResolveAccountAddProxyRequest{Phone: material.Phone})
	if err != nil || resolved.GetError() != nil || !proto.Equal(resolved.GetProxy(), material.Proxy) {
		t.Fatal("adding service could not reuse saved credentials")
	}
	other := &waappv1.WAAccount{WaAccountId: "waacc_proxy_other", Phone: &waappv1.PhoneTarget{E164Number: "+12025550199"}}
	if err := repo.SaveWAAccount(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	resolved, err = application.ResolveAccountAddProxy(t.Context(), &waappv1.ResolveAccountAddProxyRequest{Phone: other.Phone})
	if err != nil || resolved.GetError() != nil || resolved.GetProxy() != nil {
		t.Fatal("another number inherited the proxy")
	}
	material.Proxy = nil
	acceptProxyAccount(t, repo, material, "proxy-replace-login")
	login, err := repo.GetActiveLoginState(t.Context(), accountID, "")
	if err != nil {
		t.Fatal(err)
	}
	value, err := repo.(communication.ProxyResolver).GetProfileProxy(t.Context(), login.GetClientProfileId())
	if err != nil || !proto.Equal(value, fixtureProxy()) {
		t.Fatal("replacement identity lost its account proxy")
	}
	logout, err := application.LogoutWAAccount(t.Context(), &waappv1.LogoutWAAccountRequest{WaAccountId: accountID})
	if err != nil || logout.GetError() != nil {
		t.Fatal("logout failed")
	}
	repo.Close()
	repo = reopen()
	value, err = repo.GetAccountProxy(t.Context(), accountID)
	if err != nil || !proto.Equal(value, fixtureProxy()) {
		t.Fatal("logout or restart lost account proxy")
	}
	application = account.New(repo, nil, nil, nil, nil, nil)
	reset, err := application.SetAccountProxy(t.Context(), &waappv1.SetAccountProxyRequest{WaAccountId: accountID})
	if err != nil || reset.GetError() != nil || reset.GetProxy() != nil {
		t.Fatal("clearing proxy did not restore inheritance")
	}
	value, err = repo.(communication.ProxyResolver).GetProfileProxy(t.Context(), login.GetClientProfileId())
	if err != nil || value != nil {
		t.Fatal("cleared proxy still routed the client profile")
	}
	if err := repo.SaveAccountProxy(t.Context(), accountID, fixtureProxy()); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteWAAccount(t.Context(), accountID); err != nil {
		t.Fatal(err)
	}
	value, err = repo.GetAccountProxy(t.Context(), accountID)
	if err != nil || value != nil {
		t.Fatal("account deletion retained proxy credentials")
	}
}

func testAccountProxyUpdates(t *testing.T, backend string) {
	repo, _ := openRepository(t, backend)
	material := testMaterial(t)
	accepted := acceptProxyAccount(t, repo, material, "proxy-settings")
	id := accepted.GetWaAccountId()
	application := account.New(repo, nil, nil, nil, nil, nil)
	for _, tc := range []struct {
		name     string
		change   func(*waappv1.AccountProxy)
		password string
		invalid  bool
	}{
		{"preserve", func(p *waappv1.AccountProxy) { p.Password = nil }, "fixture-password", false},
		{"clear", func(p *waappv1.AccountProxy) { p.Password = &waappv1.SensitiveText{} }, "", false},
		{"replace", func(p *waappv1.AccountProxy) { p.Password.Value = "updated-fixture" }, "updated-fixture", false},
		{"new_endpoint", func(p *waappv1.AccountProxy) { p.Host = "other.example"; p.Password = nil }, "", false},
		{"new_username", func(p *waappv1.AccountProxy) { p.Username = "other"; p.Password = nil }, "", false},
		{"redacted_input", func(p *waappv1.AccountProxy) { p.Password = &waappv1.SensitiveText{RedactedValue: "********"} }, "fixture-password", true},
		{"invalid_port", func(p *waappv1.AccountProxy) { p.Port = 0 }, "fixture-password", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.SaveAccountProxy(t.Context(), id, fixtureProxy()); err != nil {
				t.Fatal(err)
			}
			requested := fixtureProxy()
			tc.change(requested)
			response, err := application.SetAccountProxy(t.Context(), &waappv1.SetAccountProxyRequest{WaAccountId: id, Proxy: requested})
			if err != nil || (response.GetError() != nil) != tc.invalid || response.GetProxy().GetPassword().GetValue() != "" {
				t.Fatal("unexpected proxy update result")
			}
			value, err := repo.GetAccountProxy(t.Context(), id)
			if err != nil || value.GetPassword().GetValue() != tc.password {
				t.Fatal("proxy update changed password semantics")
			}
			if tc.invalid && !proto.Equal(value, fixtureProxy()) {
				t.Fatal("invalid update changed stored configuration")
			}
		})
	}
}

func testAccountProxyRollback(t *testing.T, backend string) {
	repo, _ := openRepository(t, backend)
	material := testMaterial(t)
	material.Proxy = fixtureProxy()
	accepted := acceptProxyAccount(t, repo, material, "proxy-before-failure")
	material.Proxy.Host = "replacement.example"
	failed := &failingRepository{Repository: repo}
	response, err := account.New(failed, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "proxy-failure"}, Material: material})
	if err != nil || response.GetError() == nil {
		t.Fatal("injected acceptance failure was not reached")
	}
	value, err := repo.GetAccountProxy(t.Context(), accepted.GetWaAccountId())
	if err != nil || !proto.Equal(value, fixtureProxy()) {
		t.Fatal("failed acceptance replaced account proxy")
	}
}

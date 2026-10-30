package store_test

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func TestRetiredProtocolProfilesStayWithinAccount(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			accept := func(requestID string, material *waappv1.AcceptedAccountMaterial) *waappv1.LoginState {
				t.Helper()
				response, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: requestID}, Material: material})
				if err != nil || response.GetError() != nil {
					t.Fatal("seed acceptance failed")
				}
				login, err := repo.GetActiveLoginState(t.Context(), response.GetAccount().GetWaAccountId(), "")
				if err != nil {
					t.Fatal(err)
				}
				return login
			}
			original := accept("original", testMaterial(t))
			current := accept("replacement", testMaterial(t))
			other := testMaterial(t)
			other.Phone.E164Number, other.Phone.NationalNumber = "+12025550124", "2025550124"
			other.ServiceLoginId, other.Recovery.RegistrationJid = "12025550124", "12025550124@s.whatsapp.net"
			accept("other-original", other)
			accept("other-replacement", other)
			profiles, err := repo.ListRetiredProtocolProfiles(t.Context(), current.GetClientProfileId())
			if err != nil || len(profiles) != 1 || profiles[0] != original.GetClientProfileId() {
				t.Fatalf("retired profiles = %v, %v; want original identity from the same account", profiles, err)
			}
			profiles, err = repo.ListRetiredProtocolProfiles(t.Context(), "missing-profile")
			if err != nil || len(profiles) != 0 {
				t.Fatal("unknown profile exposed account history")
			}
		})
	}
}

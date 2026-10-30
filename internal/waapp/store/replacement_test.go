package store_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestConfirmedAccountReplacement(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			first, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "original"}, Material: testMaterial(t)})
			if err != nil || first.GetError() != nil {
				t.Fatal("seed acceptance failed")
			}
			id := first.GetAccount().GetWaAccountId()
			old, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			material := testMaterial(t)
			replacement, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "replacement"}, Material: material})
			if err != nil || replacement.GetError() != nil || replacement.GetAccount().GetWaAccountId() != id {
				t.Fatal("confirmed keys could not replace an existing account")
			}
			if err := repo.SaveLoginState(t.Context(), old); shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
				t.Fatal("stale heartbeat reactivated the previous keys")
			}
			app.AccountTransferredOut(t.Context(), old, nil)
			repo.Close()
			repo = reopen()
			active, err := repo.ListActiveLoginStates(t.Context())
			if err != nil || len(active) != 1 || active[0].GetRegisteredIdentityId() == old.GetRegisteredIdentityId() {
				t.Fatal("restart restored the wrong account generation")
			}
			credentials, _, err := repo.GetProtocolState(t.Context(), active[0].GetClientProfileId())
			if err != nil || !proto.Equal(credentials, material.GetCredentials()) {
				t.Fatal("replacement keys were not persisted")
			}
			records, _, err := repo.ListWAAccounts(t.Context(), "", 10)
			if err != nil || len(records) != 1 || records[0].GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE {
				t.Fatal("replacement duplicated the account or a late logout retired it")
			}
		})
	}
}

func TestReplacementRollbackPreservesCurrentLogin(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			response, _ := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "original"}, Material: testMaterial(t)})
			if response.GetError() != nil {
				t.Fatal("seed acceptance failed")
			}
			id := response.GetAccount().GetWaAccountId()
			before, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			failed := account.New(&failingRepository{Repository: repo}, nil, nil, nil, nil, nil)
			response, _ = failed.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "rollback"}, Material: testMaterial(t)})
			after, err := repo.GetActiveLoginState(t.Context(), id, "")
			if response.GetError() == nil || err != nil || !proto.Equal(before, after) {
				t.Fatal("failed replacement revoked the active login")
			}
		})
	}
}

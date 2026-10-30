package store_test

import (
	"errors"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestRetiredIdentityCannotChangeNewGeneration(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			response, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "original"}, Material: testMaterial(t)})
			if err != nil || response.GetError() != nil {
				t.Fatal("seed account failed")
			}
			id := response.GetAccount().GetWaAccountId()
			old, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			app.AccountTransferredOut(t.Context(), old, errors.New("fixture logout"))
			if err := repo.SaveLoginState(t.Context(), old); shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
				t.Fatal("stale heartbeat reactivated a retired identity")
			}
			current, err := repo.GetWAAccount(t.Context(), id)
			if err != nil || current.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT {
				t.Fatal("logout did not persist account status")
			}
			response, err = app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "replacement"}, Material: testMaterial(t)})
			if err != nil || response.GetError() != nil {
				t.Fatal("replacement account was not accepted")
			}
			app.AccountTransferredOut(t.Context(), old, errors.New("late fixture logout"))
			current, err = repo.GetWAAccount(t.Context(), id)
			if err != nil || current.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE {
				t.Fatal("late logout changed the replacement account")
			}
			active, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil || active.GetRegisteredIdentityId() == old.GetRegisteredIdentityId() {
				t.Fatal("replacement identity was not preserved")
			}
		})
	}
}

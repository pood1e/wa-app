package store_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestLoginRejectionPreservesAccountMaterialAndReplacement(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			response, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "original"}, Material: testMaterial(t)})
			if err != nil || response.GetError() != nil {
				t.Fatal("seed account failed")
			}
			id := response.GetAccount().GetWaAccountId()
			login, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			credentials, recovery, err := repo.GetProtocolState(t.Context(), login.GetClientProfileId())
			if err != nil {
				t.Fatal(err)
			}
			cause := shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED, "WA server rejected login (code 403)", false)
			app.AccountLoginRejected(t.Context(), login, cause)
			stored, err := repo.GetLoginState(t.Context(), login.GetLoginStateId())
			if err != nil || stored.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID || stored.GetLastError().GetRetryable() {
				t.Fatal("rejected login remained active or retryable")
			}
			if err := repo.SaveLoginState(t.Context(), login); shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
				t.Fatal("stale successful heartbeat reactivated a rejected identity")
			}
			stopped, err := repo.ListStoppedLoginStates(t.Context())
			if err != nil || len(stopped) != 1 || stopped[0].GetLoginStateId() != login.GetLoginStateId() {
				t.Fatal("rejected login is missing from restart restoration")
			}
			savedCredentials, savedRecovery, err := repo.GetProtocolState(t.Context(), login.GetClientProfileId())
			if err != nil || !proto.Equal(credentials, savedCredentials) || !proto.Equal(recovery, savedRecovery) {
				t.Fatal("login rejection changed protocol material")
			}
			response, err = app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "replacement"}, Material: testMaterial(t)})
			if err != nil || response.GetError() != nil {
				t.Fatal("replacement account was not accepted")
			}
			app.AccountLoginRejected(t.Context(), login, cause)
			active, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil || active.GetRegisteredIdentityId() == login.GetRegisteredIdentityId() {
				t.Fatal("late login rejection changed the replacement identity")
			}
		})
	}
}

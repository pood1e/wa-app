package store_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestAccountReviewRecoveryAndLatestGeneration(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			result, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "review-original"}, Material: testMaterial(t)})
			if err != nil || result.GetError() != nil {
				t.Fatal("seed account failed")
			}
			accountID := result.GetAccount().GetWaAccountId()
			original, err := repo.GetLatestAccountLoginState(t.Context(), accountID)
			if err != nil {
				t.Fatal(err)
			}
			keys, recovery, err := repo.GetProtocolState(t.Context(), original.GetClientProfileId())
			if err != nil {
				t.Fatal(err)
			}
			recovery.AccountReview = &protocolv1.AccountReviewState{Token: "synthetic-review-authorization", Review: &protocolv1.AccountReview{Status: protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW, CheckedAt: timestamppb.Now()}}
			if err := repo.SaveProtocolState(t.Context(), original.GetClientProfileId(), keys, recovery); err != nil {
				t.Fatal(err)
			}
			repo.Close()
			repo = reopen()
			storedKeys, storedRecovery, err := repo.GetProtocolState(t.Context(), original.GetClientProfileId())
			if err != nil || !proto.Equal(keys, storedKeys) || !proto.Equal(recovery, storedRecovery) {
				t.Fatal("restart changed credentials or lost review authorization and status")
			}
			app = account.New(repo, nil, nil, nil, nil, nil)
			result, err = app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "review-replacement"}, Material: testMaterial(t)})
			if err != nil || result.GetError() != nil {
				t.Fatal("replacement account failed")
			}
			replacement, err := repo.GetActiveLoginState(t.Context(), accountID, "")
			if err != nil {
				t.Fatal(err)
			}
			original, err = repo.GetLoginState(t.Context(), original.GetLoginStateId())
			if err != nil {
				t.Fatal(err)
			}
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			original.RegisteredAt = timestamppb.New(base)
			original.Audit.UpdatedAt = timestamppb.New(base.Add(time.Hour))
			replacement.RegisteredAt = timestamppb.New(base.Add(500 * time.Millisecond))
			for _, login := range []*waappv1.LoginState{original, replacement} {
				if err := repo.SaveLoginState(t.Context(), login); err != nil {
					t.Fatal(err)
				}
			}
			latest, err := repo.GetLatestAccountLoginState(t.Context(), accountID)
			if err != nil || latest.GetLoginStateId() != replacement.GetLoginStateId() {
				t.Fatal("a late update or fractional timestamp selected the superseded login")
			}
			if _, err := repo.GetLatestAccountLoginState(t.Context(), "waacc_missing"); shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND {
				t.Fatal("missing account login did not return the expected error")
			}
		})
	}
}

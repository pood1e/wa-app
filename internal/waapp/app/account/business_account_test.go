package account

import (
	"testing"
	"time"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountUpdatesPreserveBusinessType(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	account := &waappv1.WAAccount{WaAccountId: "fixture", AccountType: modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS, Status: waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE, Audit: &waappv1.AuditStamp{CreatedAt: timestamppb.New(before)}}
	for _, updated := range []*waappv1.WAAccount{
		withWAAccountDisplayName(account, "WA Lab", time.Now()),
		withWAAccountStatus(account, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT, time.Now()),
		withWAAccountTwoFactorAuthStatus(account, &waappv1.TwoFactorAuthStatus{Configured: true}, time.Now()),
	} {
		if updated.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS || updated.GetWaAccountId() != account.GetWaAccountId() || !updated.GetAudit().GetCreatedAt().AsTime().Equal(before) {
			t.Fatal("an account update changed its business type, identity, or original creation time")
		}
	}
}

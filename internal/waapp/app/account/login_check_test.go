package account

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUnreachableLoginCheckPreservesAuthenticatedIdentity(t *testing.T) {
	verified := timestamppb.New(time.Unix(10, 0))
	login := &waappv1.LoginState{Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE, LastVerifiedAt: verified}
	check := &waappv1.LoginStateCheck{
		Status:    waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE,
		CheckedAt: timestamppb.New(time.Unix(20, 0)),
		Error:     &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, Message: "login heartbeat response timed out", Retryable: true},
	}
	if !new(serverCore).applyLoginStateCheck(login, check, time.Unix(20, 0)) {
		t.Fatal("unreachable check was not recorded")
	}
	if login.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE || !login.GetLastVerifiedAt().AsTime().Equal(verified.AsTime()) || login.GetLastError() != check.GetError() {
		t.Fatal("heartbeat failure changed the authenticated identity or lost the temporary error")
	}
}

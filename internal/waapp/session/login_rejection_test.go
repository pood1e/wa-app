package session

import (
	"context"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type rejectedLoginHost struct {
	lifecycleHost
	manager  *Manager
	rejected int
}

func (h *rejectedLoginHost) ReceiveWithProtocol(context.Context, *waappv1.ReceiveMessageBatchRequest, wacore.Communication) (*waappv1.ReceiveMessageBatchResponse, error) {
	return &waappv1.ReceiveMessageBatchResponse{Error: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED, Message: "WA server rejected login (code 403)"}}, nil
}

func (h *rejectedLoginHost) AccountLoginRejected(_ context.Context, login *waappv1.LoginState, cause error) {
	h.rejected++
	h.manager.Revoke(login.GetRegisteredIdentityId(), cause)
}

func TestLoginRejectionStopsOwnedConnectionAndRestoresFailure(t *testing.T) {
	manager, trace, _ := newLifecycleManager()
	host := &rejectedLoginHost{lifecycleHost: lifecycleHost{trace: trace}, manager: manager}
	manager.host = host
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	login := fixtureLogin()
	key := longConnectionKey(login)
	manager.rootCtx = ctx
	manager.entries[key] = &longConnectionEntry{cancel: cancel, snapshot: &waappv1.LongConnectionState{RegisteredIdentityId: login.GetRegisteredIdentityId()}}
	manager.runEntry(ctx, login, key)
	if host.rejected != 1 {
		t.Fatalf("persisted rejections = %d, want 1", host.rejected)
	}
	assertLifecycle(t, trace, "")
	login.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID
	login.LastError = &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED, Message: "WA server rejected login (code 403)"}
	restored := New(nil, nil, nil, shared.SystemClock{}, shared.RandomIDGenerator{})
	restored.rootCtx = t.Context()
	restored.seedStoppedEntry(login)
	snapshots := restored.Snapshots(&waappv1.GetLongConnectionStatusRequest{})
	if len(snapshots) != 1 || snapshots[0].GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED || snapshots[0].GetLastError().GetCode() != login.GetLastError().GetCode() || restored.Runner(login) != nil {
		t.Fatal("restart lost the terminal rejection or restarted the connection")
	}
}

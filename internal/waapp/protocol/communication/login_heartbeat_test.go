package communication

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestLoginCheckRequiresWhatsAppHeartbeatResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	session, peer := syntheticChatdPair(t)
	connection := NewConnection(nil, ConnectionOptions{Context: ctx})
	connection.Session = session
	t.Cleanup(func() { _ = connection.Close() })
	finished := make(chan error, 1)
	go func() {
		request, err := peer.ReadNode()
		if err != nil {
			finished <- err
			return
		}
		response := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": request.Attrs["id"], "type": "result"}}
		if request.Tag != "iq" || request.Attrs["type"] != "get" || request.Attrs["to"] != "s.whatsapp.net" || request.Attrs["xmlns"] != "w:p" || len(wire.ChatdChildren(request)) != 0 {
			response.Attrs["type"] = "error"
			response.Content = []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "501"}}}
		}
		finished <- peer.SendNode(response)
	}()
	result := connection.CheckLoginState(ctx, wacore.EngineLoginCheckInput{RemoteTimeout: time.Second})
	if result.Err != nil || result.Status != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE {
		t.Fatalf("heartbeat check = %s, %v; want ACTIVE", result.Status, result.Err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestLoginCheckInvalidatesOnlyDefinitiveRejections(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want waappv1.LoginStateCheckStatus
	}{
		{"authenticated", nil, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE},
		{"heartbeat timeout", errors.New("login heartbeat response timed out"), waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"canceled check", context.Canceled, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"deadline", context.DeadlineExceeded, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"closed connection", io.EOF, waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"IQ rejection", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "IQ request was rejected", false), waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE},
		{"authentication rejection", chatdAuthenticationFailure(wire.ChatdNode{Tag: "failure", Attrs: map[string]string{"reason": "403"}}), waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID},
		{"identity replaced", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, ChatdAccountTakeoverMarker, false), waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := LoginCheckStatusForError(test.err); got != test.want {
				t.Fatalf("login check = %s, want %s", got, test.want)
			}
		})
	}
}

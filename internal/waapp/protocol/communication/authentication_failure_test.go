package communication

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestAuthorizationRejectionRemainsTerminalThroughErrorAdapters(t *testing.T) {
	for _, reason := range []string{"401", "402", "403", "405", "406", "409", "413", "414"} {
		t.Run(reason, func(t *testing.T) {
			cause := chatdAuthenticationFailure(wire.ChatdNode{Tag: "failure", Attrs: map[string]string{"reason": reason}})
			wrapped := fmt.Errorf("endpoint failed: %w", cause)
			for _, err := range []error{wrapped, ChatdReceiveError(wrapped), ChatdSendError(wrapped)} {
				if !IsAuthenticationFailure(err) || shared.ToProtoError(err).GetRetryable() || IsAccountTakeoverError(err) {
					t.Fatal("authorization rejection became retryable or a transfer event")
				}
				if LoginCheckStatusForError(err) != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID {
					t.Fatal("rejected login was not invalidated")
				}
			}
		})
	}
	if chatdAuthenticationFailure(wire.ChatdNode{Tag: "stream:error", Attrs: map[string]string{"code": "403"}}) != nil {
		t.Fatal("a stream error was mistaken for a login rejection")
	}
}

func TestReviewAuthorizationStaysPrivateThroughErrors(t *testing.T) {
	const token = "synthetic-private-review-token"
	cause := chatdAuthenticationFailure(wire.ChatdNode{Tag: "failure", Attrs: map[string]string{"reason": "403", "appeal_token": token}})
	wrapped := fmt.Errorf("endpoint failed: %w", cause)
	var rejection *loginRejection
	if !errors.As(wrapped, &rejection) || rejection.review.GetToken() != token {
		t.Fatal("login rejection lost review authorization")
	}
	if strings.Contains(wrapped.Error(), token) || strings.Contains(shared.ToProtoError(wrapped).GetMessage(), token) {
		t.Fatal("login rejection exposed private review authorization")
	}
}

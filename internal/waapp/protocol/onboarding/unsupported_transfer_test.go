package onboarding

import (
	"context"
	"errors"
	"net"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestQRTransferCannotSendNativeRequests(t *testing.T) {
	flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, protocoltest.AndroidBuild(t), protocoltest.AndroidClient(), "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	flow.http.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("unsupported QR migration attempted an outbound connection")
		return nil, errors.New("unexpected connection")
	}
	method := waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	input := flow.input(method)
	request := flow.RequestCode(t.Context(), method)
	native, updated := flow.RequestVerificationCodeWithState(t.Context(), input, flow.current)
	submit := flow.SubmitCode(t.Context(), method, "")
	refresh := flow.RefreshTransfer(t.Context())
	for _, err := range []error{request.Err, native.Err, submit.Err, refresh.Err} {
		if shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION {
			t.Fatal("unsupported migration was treated as available")
		}
	}
	if updated.GenerateCodeAttempts != 0 {
		t.Fatal("unsupported migration mutated registration state")
	}
	if result := flow.SubmitVerificationCode(t.Context(), wacore.EngineSubmitInput{EngineRegistrationInput: input}); shared.ToProtoError(result.Err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION {
		t.Fatal("native migration submission bypassed capability validation")
	}
}

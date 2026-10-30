package onboarding

import (
	"context"
	"errors"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestUncertainRegistrationAutomaticallyConfirmsLogin(t *testing.T) {
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		for _, transfer := range []bool{false, true} {
			t.Run(platform.String()+map[bool]string{false: "/registration", true: "/transfer"}[transfer], func(t *testing.T) {
				service, protocol, acceptor := testService(t)
				selected := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP, Host: "proxy.example", Port: 8080}
				service.factory = func(context.Context, *waappv1.BeginAccountAddRequest) (Protocol, error) {
					return WithAccountProxy(protocol, selected), nil
				}
				checks := 0
				service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
					checks++
					if protocol.closes.Load() != 0 || !proto.Equal(material.GetProxy(), selected) {
						t.Error("automatic confirmation lost the original protocol or selected proxy")
					}
					return material, nil
				}
				request := beginRequest(transfer)
				request.Platform = platform
				response, _ := service.BeginAccountAdd(t.Context(), request)
				id := response.GetFlow().GetFlowId()
				method := sms
				if transfer {
					method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
				}
				response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: method})
				if response.GetError() != nil {
					t.Fatal("code delivery failed")
				}
				protocol.submitError = errors.New("upstream HTTP 502 after registration")
				submit := &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"}
				var requests sync.WaitGroup
				for range 4 {
					requests.Go(func() {
						response, err := service.SubmitAccountAddCode(t.Context(), submit)
						if err != nil || response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
							t.Error("confirmed login did not complete adding in the original submission")
						}
					})
				}
				requests.Wait()
				if checks != 1 || protocol.probes.Load() != 1 || protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || protocol.closes.Load() != 1 || acceptor.calls.Load() != 1 {
					t.Fatal("automatic confirmation repeated verification, registration, validation or acceptance")
				}
				current := service.lookup(id)
				if current.protocol != nil || current.material != nil || len(current.operations) != 0 || current.snapshot.GetLastError() != nil {
					t.Fatal("successful automatic confirmation retained temporary material or an error")
				}
			})
		}
	}
}

func TestUncertainPasswordAutomaticallyConfirmsLogin(t *testing.T) {
	service, protocol, id := passwordFlow(t)
	checks := 0
	service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		checks++
		return material, nil
	}
	protocol.passwordResult = &wacore.EngineRegisterResult{OutcomeUnknown: true, Err: errors.New("interrupted password response")}
	request := &waappv1.SubmitAccountAddPasswordRequest{Context: requestContext("password"), FlowId: id, Password: &waappv1.SensitiveText{Value: "Password1"}}
	for range 2 {
		response, err := service.SubmitAccountAddPassword(t.Context(), request)
		if err != nil || response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || response.GetFlow().GetPasswordChallenge() != nil {
			t.Fatal("confirmed password login required a separate recovery action")
		}
	}
	if checks != 1 || protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || protocol.passwords.Load() != 1 {
		t.Fatal("password recovery repeated a verification request")
	}
}

func TestDefinitiveRegistrationRejectionDoesNotAttemptLogin(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	id := response.GetFlow().GetFlowId()
	_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD})
	protocol.submitResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: errors.New("verification rejected")}
	service.validate = func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		t.Error("definitively rejected registration attempted login")
		return nil, nil
	}
	response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"})
	if response.GetError() == nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE || protocol.closes.Load() != 0 || acceptor.calls.Load() != 0 {
		t.Fatal("registration rejection was hidden or the flow was discarded")
	}
}

func TestCanceledSubmissionDoesNotStartAutomaticLogin(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	protocol.submitError = context.Canceled
	service.validate = func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		t.Error("canceled submission started another network operation")
		return nil, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, _ = service.SubmitAccountAddCode(ctx, &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"})
	if response.GetError() == nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || service.lookup(id).protocol != protocol || acceptor.calls.Load() != 0 {
		t.Fatal("canceled submission discarded its uncertain outcome or accepted an unconfirmed login")
	}
}

func TestAutomaticConfirmationRecoversLostAcceptanceReply(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	id := response.GetFlow().GetFlowId()
	_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD})
	protocol.submitError = errors.New("upstream HTTP 502")
	acceptor.loseReply = true
	checks := 0
	service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		checks++
		return material, nil
	}
	response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"})
	if response.GetError().GetMessage() != "acceptance reply lost" || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY_TO_DELIVER {
		t.Fatal("automatic confirmation hid a failed handoff or returned the original registration error")
	}
	response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("retry-delivery"), FlowId: id})
	if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		t.Fatal("confirmed registration could not recover its acceptance receipt")
	}
	if checks != 1 || protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || protocol.closes.Load() != 1 || acceptor.calls.Load() != 1 {
		t.Fatal("receipt recovery repeated verification, validation or acceptance")
	}
}

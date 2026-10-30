package onboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestUncertainRegistrationRecoversWithoutAnotherSubmission(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	id := response.GetFlow().GetFlowId()
	_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD})
	protocol.submitError = errors.New("upstream HTTP 502")
	validationErr := errors.New("login not confirmed")
	checks := 0
	service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		checks++
		return material, validationErr
	}
	response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "123456"})
	if response.GetError() == nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || checks != 1 {
		t.Fatal("failed automatic confirmation discarded the uncertain registration")
	}
	response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("check-failed"), FlowId: id})
	if response.GetError() == nil || protocol.closes.Load() != 0 || acceptor.calls.Load() != 0 || service.lookup(id).protocol != protocol {
		t.Fatal("failed authentication discarded the original flow or accepted an unproven account")
	}
	validationErr = nil
	request := &waappv1.AccountAddRequest{Context: requestContext("check-accepted"), FlowId: id}
	for range 2 {
		response, _ = service.DeliverAccountAdd(t.Context(), request)
		if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
			t.Fatal("confirmed login did not complete delivery")
		}
	}
	if protocol.submits.Load() != 1 || protocol.codes.Load() != 1 || protocol.closes.Load() != 1 || acceptor.calls.Load() != 1 || checks != 3 {
		t.Fatal("recovery repeated registration, code delivery or acceptance")
	}
}

func TestRecoveryRequiresConfirmedCodeDelivery(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	protocol.codeError = errors.New("unconfirmed code delivery")
	_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	service.validate = func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		t.Fatal("login recovery ran without a registration submission")
		return nil, nil
	}
	response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("check"), FlowId: id})
	if response.GetError() == nil || acceptor.calls.Load() != 0 {
		t.Fatal("unconfirmed code request was accepted")
	}
}

func TestQRTransferRejectedBeforeRequestingCodes(t *testing.T) {
	service, protocol, _ := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("qr"), FlowId: response.GetFlow().GetFlowId(), DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER})
	if response.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION || protocol.codes.Load() != 0 {
		t.Fatal("incomplete QR migration sent a verification request")
	}
}

func TestExpiredQRDoesNotBlockAuthenticationOfRetainedKeys(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	id := response.GetFlow().GetFlowId()
	current := service.lookup(id)
	current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE
	current.snapshot.Verification = &waappv1.VerificationCodeRequestRecord{DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER, Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT}
	current.snapshot.TransferChallenge = &waappv1.AccountTransferChallenge{ExpiresAt: timestamppb.New(time.Now().Add(-time.Minute))}
	service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		return material, nil
	}
	response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("recover-expired-qr"), FlowId: id})
	if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		t.Fatal("expired QR prevented authentication and acceptance")
	}
	if protocol.codes.Load() != 0 || protocol.submits.Load() != 0 || acceptor.calls.Load() != 1 {
		t.Fatal("expired QR recovery submitted another verification or registration request")
	}
}

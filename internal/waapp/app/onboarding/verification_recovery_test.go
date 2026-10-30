package onboarding

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestVerificationRecoveryRetainsFlowAndCooldown(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(map[bool]string{false: "sms", true: "wa_old"}[transfer], func(t *testing.T) {
			service, protocol, acceptor := testService(t)
			clock := &verificationClock{now: time.Now()}
			service.clock = clock
			response, _ := service.BeginAccountAdd(t.Context(), beginRequest(transfer))
			id := response.GetFlow().GetFlowId()
			method := sms
			if transfer {
				method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
			}
			requestCode := func(requestID string) *waappv1.AccountAddResponse {
				response, _ := service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext(requestID), FlowId: id, DeliveryMethod: method})
				return response
			}
			submit := func(requestID string) *waappv1.AccountAddResponse {
				response, _ := service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext(requestID), FlowId: id, Code: "012345"})
				return response
			}
			protocol.codeResult = &wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
				RawStatus: "fail", RawReason: "temporarily_unavailable", RetryAfter: time.Minute,
				Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "synthetic cooldown", true)}
			response = requestCode("initial-unavailable")
			if response.GetError() == nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY {
				t.Fatal("rejected delivery entered the code input stage")
			}
			if submit("unconfirmed-code").GetError() == nil || protocol.submits.Load() != 0 {
				t.Fatal("rejected delivery allowed registration")
			}
			clock.now = clock.now.Add(time.Minute)
			protocol.codeResult = &wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT, ExpectedCodeLength: 6, RetryAfter: 2 * time.Minute}
			response = requestCode("confirmed-code")
			if response.GetError() != nil {
				t.Fatal("confirmed code request failed")
			}
			verificationID := response.GetFlow().GetVerification().GetVerificationRequestId()
			clock.now = clock.now.Add(30 * time.Second)
			serverCooldown := 15 * time.Second
			if transfer {
				serverCooldown = 3 * time.Minute
			}
			remaining := max(90*time.Second, serverCooldown)
			protocol.submitResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE, RetryAfter: serverCooldown,
				Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "synthetic missing verification", true)}
			for range 2 {
				response = submit("missing-code")
				if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY || response.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED {
					t.Fatal("verification rejection did not return to channel selection")
				}
			}
			verification := response.GetFlow().GetVerification()
			if verification.GetStatus() != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED || verification.GetRetryAfter().AsDuration() != 30*time.Second+remaining || protocol.submits.Load() != 1 {
				t.Fatal("verification recovery lost its cooldown or replayed the submission")
			}
			if submit("repeat-stale-code").GetError() == nil || protocol.submits.Load() != 1 {
				t.Fatal("stale code was submitted without a fresh request")
			}
			if requestCode("too-soon").GetError() == nil || protocol.codes.Load() != 2 {
				t.Fatal("verification recovery bypassed the server cooldown")
			}
			if current := service.lookup(id); current.protocol != protocol || protocol.closes.Load() != 0 || acceptor.calls.Load() != 0 {
				t.Fatal("verification recovery replaced protocol material or accepted an unverified account")
			}
			clock.now = clock.now.Add(remaining)
			protocol.codeResult = nil
			response = requestCode("fresh-code")
			if response.GetError() != nil || response.GetFlow().GetVerification().GetVerificationRequestId() == verificationID {
				t.Fatal("explicit fresh request did not create a new verification record")
			}
			protocol.submitResult = nil
			response = submit("fresh-submit")
			if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
				t.Fatal("fresh verification did not complete delivery")
			}
			if protocol.probes.Load() != 1 || protocol.codes.Load() != 3 || protocol.submits.Load() != 2 || acceptor.calls.Load() != 1 {
				t.Fatal("recovery repeated preparation, registration, or acceptance")
			}
		})
	}
}

func TestWaitingCodeRequiresConfirmedVerification(t *testing.T) {
	for _, verification := range []*waappv1.VerificationCodeRequestRecord{
		nil, {}, {Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED},
	} {
		service, protocol, _ := testService(t)
		response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
		id := response.GetFlow().GetFlowId()
		current := service.lookup(id)
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE
		current.snapshot.Verification = verification
		response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("unconfirmed-submit"), FlowId: id, Code: "012345"})
		if response.GetError() == nil || protocol.submits.Load() != 0 {
			t.Fatal("waiting-code state bypassed delivery confirmation")
		}
	}
}

type verificationClock struct{ now time.Time }

func (c *verificationClock) Now() time.Time { return c.now }

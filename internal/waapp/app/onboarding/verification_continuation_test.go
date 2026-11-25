package onboarding

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"google.golang.org/protobuf/proto"
)

func TestRequiredVerificationContinuesAcrossTheOriginalSource(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(map[bool]string{false: "registration to old device", true: "old device to SMS"}[transfer], func(t *testing.T) {
			service, protocol, acceptor := testService(t)
			clock := &verificationClock{now: time.Unix(1_800_000_000, 0)}
			service.clock = clock
			initial, required := sms, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
			if transfer {
				initial, required = required, sms
			}
			response, _ := service.BeginAccountAdd(t.Context(), beginRequest(transfer))
			id := response.GetFlow().GetFlowId()
			source := proto.CloneOf(response.GetFlow().GetSource())
			request := func(key string, method waappv1.VerificationDeliveryMethod) *waappv1.AccountAddResponse {
				response, _ := service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext(key), FlowId: id, DeliveryMethod: method})
				return response
			}
			submit := func(key string) *waappv1.AccountAddResponse {
				response, _ := service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext(key), FlowId: id, Code: "012345"})
				return response
			}
			protocol.codeResult = &wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT, ExpectedCodeLength: 6, RetryAfter: 2 * time.Minute}
			if response := request("first-code", initial); response.GetError() != nil {
				t.Fatal("initial verification request failed")
			}
			clock.now = clock.now.Add(30 * time.Second)
			protocol.submitResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE,
				NextVerification: &waappv1.VerificationMethodStatus{DeliveryMethod: required, Cooldown: shared.DurationToProto(3 * time.Minute)},
				Err:              shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "additional verification is required", true)}
			for range 2 {
				response = submit("first-submit")
				if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY || response.GetFlow().GetRequiredVerificationMethod() != required || !proto.Equal(response.GetFlow().GetSource(), source) {
					t.Fatal("continuation discarded its required method or changed the original source")
				}
			}
			if protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || acceptor.calls.Load() != 0 {
				t.Fatal("continuation resent a code, replayed verification, or accepted an unfinished account")
			}
			if submit("used-proof").GetError() == nil || request("premature-next", required).GetError() == nil {
				t.Fatal("continuation reused a proof or bypassed its cooldown")
			}
			clock.now = clock.now.Add(3 * time.Minute)
			if request("wrong-method", initial).GetError() == nil || protocol.codes.Load() != 1 {
				t.Fatal("continuation accepted a different method")
			}
			protocol.codeResult = nil
			if response := request("required-code", required); response.GetError() != nil {
				t.Fatal("explicit required verification failed")
			}
			protocol.submitResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE,
				Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "stale verification", true)}
			submit("stale-next")
			if response := request("fresh-required-code", required); response.GetError() != nil {
				t.Fatal("a stale next-step proof discarded the required channel")
			}
			protocol.submitResult = nil
			response = submit("final-proof")
			if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || response.GetFlow().GetRequiredVerificationMethod() != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
				t.Fatal("completed continuation retained restrictions or failed acceptance")
			}
			if protocol.probes.Load() != 1 || protocol.codes.Load() != 3 || acceptor.calls.Load() != 1 || protocol.closes.Load() != 1 {
				t.Fatal("continuation repeated preparation, delivery, or cleanup")
			}
		})
	}
}

func TestFinishedFlowClearsRequiredVerification(t *testing.T) {
	current := &flow{snapshot: &waappv1.AccountAddFlow{RequiredVerificationMethod: sms}}
	current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_CANCELLED)
	if current.snapshot.GetRequiredVerificationMethod() != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		t.Fatal("finished flow retained a verification requirement")
	}
}

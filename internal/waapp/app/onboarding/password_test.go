package onboarding

import (
	"errors"
	"strings"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func passwordFlow(t *testing.T) (*Service, *testProtocol, string) {
	t.Helper()
	service, protocol, _ := testService(t)
	protocol.submitResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD, PasswordChallenge: &waappv1.AccountAddPasswordChallenge{}}
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	if response.GetError() != nil {
		t.Fatal("request code failed")
	}
	response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("first-factor"), FlowId: id, Code: "123456"})
	if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_PASSWORD || response.GetFlow().GetPasswordChallenge() == nil {
		t.Fatal("successful first factor did not retain the password step")
	}
	return service, protocol, id
}

func TestPasswordContinuesOneFlowWithoutResendingFirstFactor(t *testing.T) {
	service, protocol, id := passwordFlow(t)
	const secret = " PRIVATE_PASSWORD_SENTINEL9 "
	protocol.password = func(value string) {
		if value != secret {
			t.Error("password text was changed")
		}
	}
	request := &waappv1.SubmitAccountAddPasswordRequest{Context: requestContext("password"), FlowId: id, Password: &waappv1.SensitiveText{Value: secret}}
	for range 2 {
		response, _ := service.SubmitAccountAddPassword(t.Context(), request)
		encoded, err := protojson.Marshal(response)
		if err != nil || strings.Contains(string(encoded), secret) {
			t.Fatal("password escaped in a response")
		}
		if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || response.GetFlow().GetPasswordChallenge() != nil {
			t.Fatal("password verification did not complete account acceptance")
		}
	}
	if protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || protocol.passwords.Load() != 1 || protocol.closes.Load() != 1 {
		t.Fatal("password verification repeated registration, code delivery or cleanup")
	}
}

func TestPasswordUnknownOutcomeRetainsItsStepForExplicitResubmission(t *testing.T) {
	service, protocol, id := passwordFlow(t)
	protocol.passwordResult = &wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, OutcomeUnknown: true, Err: errors.New("interrupted password response")}
	request := &waappv1.SubmitAccountAddPasswordRequest{Context: requestContext("password"), FlowId: id, Password: &waappv1.SensitiveText{Value: "Password1"}}
	for range 2 {
		response, _ := service.SubmitAccountAddPassword(t.Context(), request)
		if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || response.GetFlow().GetPasswordChallenge() == nil {
			t.Fatal("unknown password outcome lost the verification step")
		}
	}
	if protocol.passwords.Load() != 1 || service.lookup(id).protocol != protocol {
		t.Fatal("unknown outcome replaced or replayed the flow")
	}
	protocol.passwordResult = nil
	request.Context = requestContext("explicit-password-resubmission")
	response, _ := service.SubmitAccountAddPassword(t.Context(), request)
	if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || protocol.passwords.Load() != 2 || protocol.codes.Load() != 1 {
		t.Fatal("explicit password resubmission did not use retained material")
	}
}

func TestPasswordChallengeRejectsOTPAndHonorsGuessCooldown(t *testing.T) {
	service, protocol, id := passwordFlow(t)
	response, _ := service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("another-otp"), FlowId: id, Code: "123456"})
	if response.GetError() == nil || protocol.submits.Load() != 1 {
		t.Fatal("password challenge accepted another OTP")
	}
	service.lookup(id).snapshot.PasswordChallenge.NextAttemptAt = timestamppb.New(time.Now().Add(time.Hour))
	response, _ = service.SubmitAccountAddPassword(t.Context(), &waappv1.SubmitAccountAddPasswordRequest{Context: requestContext("early-password"), FlowId: id, Password: &waappv1.SensitiveText{Value: "Password1"}})
	if response.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED || protocol.passwords.Load() != 0 {
		t.Fatal("password cooldown made a remote request")
	}
	response, _ = service.CancelAccountAdd(t.Context(), &waappv1.AccountAddRequest{FlowId: id})
	if response.GetFlow().GetPasswordChallenge() != nil || service.lookup(id).protocol != nil {
		t.Fatal("cancellation retained the password challenge")
	}
}

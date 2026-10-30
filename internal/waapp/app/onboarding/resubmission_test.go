package onboarding

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestExplicitCodeResubmissionAfterUnknownRegistration(t *testing.T) {
	service, protocol, acceptor := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	if response.GetError() != nil {
		t.Fatal("request code failed")
	}
	request := &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "000000"}
	protocol.submitError = errors.New("upstream HTTP 502")
	response, _ = service.SubmitAccountAddCode(t.Context(), request)
	if response.GetError() == nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN {
		t.Fatal("unknown registration outcome was not retained")
	}
	current := service.lookup(id)
	if current.protocol != protocol || protocol.closes.Load() != 0 {
		t.Fatal("registration failure discarded the original protocol material")
	}
	for range 3 {
		response, _ = service.SubmitAccountAddCode(t.Context(), request)
		if response.GetError() == nil || protocol.submits.Load() != 1 {
			t.Fatal("duplicate request repeated an uncertain submission")
		}
	}
	protocol.submitError = nil
	resubmission := proto.Clone(request).(*waappv1.SubmitAccountAddCodeRequest)
	resubmission.Context = requestContext("explicit-resubmission")
	response, _ = service.SubmitAccountAddCode(t.Context(), resubmission)
	if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		t.Fatal("explicit resubmission did not complete account delivery")
	}
	if protocol.probes.Load() != 1 || protocol.codes.Load() != 1 || protocol.submits.Load() != 2 || acceptor.calls.Load() != 1 {
		t.Fatal("resubmission repeated protocol preparation, code delivery or acceptance")
	}
}

func TestUnknownCodeDeliveryCannotSubmitRegistration(t *testing.T) {
	service, protocol, _ := testService(t)
	protocol.codeError = errors.New("unknown code delivery outcome")
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN {
		t.Fatal("code delivery outcome was not retained")
	}
	response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "000000"})
	if response.GetError() == nil || protocol.submits.Load() != 0 {
		t.Fatal("registration was submitted without a confirmed verification request")
	}
}

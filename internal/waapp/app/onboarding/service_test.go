package onboarding

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestConcurrentFlowAndCodeRetries(t *testing.T) {
	service, protocol, _ := testService(t)
	begin := beginRequest(false)
	initial, _ := service.BeginAccountAdd(t.Context(), begin)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			response, _ := service.BeginAccountAdd(t.Context(), begin)
			if response.GetError() != nil || response.GetFlow().GetFlowId() != initial.GetFlow().GetFlowId() {
				t.Error("begin retry did not reuse the flow")
			}
		})
	}
	workers.Wait()
	if protocol.probes.Load() != 1 {
		t.Fatal("begin retries repeated protocol preparation")
	}
	request := &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: initial.GetFlow().GetFlowId(), DeliveryMethod: sms}
	for range 8 {
		workers.Go(func() {
			response, _ := service.RequestAccountAddCode(t.Context(), request)
			if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE {
				t.Error("code retry failed")
			}
		})
	}
	workers.Wait()
	if protocol.codes.Load() != 1 {
		t.Fatal("duplicate request resent the verification code")
	}
	changed := proto.Clone(request).(*waappv1.RequestAccountAddCodeRequest)
	changed.DeliveryMethod = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	rejected, _ := service.RequestAccountAddCode(t.Context(), changed)
	if rejected.GetError() == nil || protocol.codes.Load() != 1 {
		t.Fatal("request ID reuse was accepted")
	}
}

func TestUnknownCodeOutcomeIsNotReplayed(t *testing.T) {
	service, protocol, _ := testService(t)
	protocol.codeError = errors.New("transport outcome unknown")
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	for _, requestID := range []string{"code", "code", "another-code"} {
		response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext(requestID), FlowId: id, DeliveryMethod: sms})
		if response.GetError() == nil {
			t.Fatal("unknown outcome was not reported")
		}
	}
	if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || protocol.codes.Load() != 1 {
		t.Fatal("uncertain operation was replayed")
	}
}

func TestLostAcceptanceReplyIsRecoveredWithoutReregistering(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(map[bool]string{false: "registration", true: "transfer"}[transfer], func(t *testing.T) {
			service, protocol, acceptor := testService(t)
			acceptor.loseReply = true
			response, _ := service.BeginAccountAdd(t.Context(), beginRequest(transfer))
			id := response.GetFlow().GetFlowId()
			method := sms
			if transfer {
				method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
			}
			response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: method})
			if response.GetError() != nil {
				t.Fatal("request code failed")
			}
			response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "123456"})
			if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY_TO_DELIVER || response.GetError() == nil {
				t.Fatal("lost reply did not retain pending delivery")
			}
			response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("retry-delivery"), FlowId: id})
			if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
				t.Fatal("acceptance receipt recovery failed")
			}
			if protocol.submits.Load() != 1 || acceptor.calls.Load() != 1 || protocol.closes.Load() != 1 {
				t.Fatal("delivery retry repeated registration or acceptance")
			}
			current := service.lookup(id)
			if current.material != nil || current.protocol != nil || len(current.operations) != 0 || current.snapshot.GetTransferChallenge() != nil {
				t.Fatal("completed flow retained temporary material")
			}
		})
	}
}

func TestCancellationInterruptsProtocolAndCleansMaterial(t *testing.T) {
	service, protocol, _ := testService(t)
	protocol.started = make(chan struct{})
	protocol.block = true
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: sms})
	}()
	<-protocol.started
	response, _ = service.CancelAccountAdd(t.Context(), &waappv1.AccountAddRequest{FlowId: id})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt the protocol")
	}
	if response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_CANCELLED || protocol.closes.Load() != 1 {
		t.Fatal("cancel did not clean the flow")
	}
}

func TestExpiryAndRestartDiscardIncompleteFlows(t *testing.T) {
	service, protocol, _ := testService(t)
	service.ttl = 5 * time.Millisecond
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(false))
	id := response.GetFlow().GetFlowId()
	<-service.lookup(id).ctx.Done()
	service.expire()
	if service.lookup(id) != nil || protocol.closes.Load() != 1 {
		t.Fatal("expiry retained material")
	}
	restarted, _, _ := testService(t)
	response, _ = restarted.GetAccountAdd(t.Context(), &waappv1.AccountAddRequest{FlowId: id})
	if response.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED {
		t.Fatal("restart recovered an in-memory flow")
	}
}

func TestAddingSourceConstrainsVerificationMethod(t *testing.T) {
	service, protocol, _ := testService(t)
	response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
	response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: response.GetFlow().GetFlowId(), DeliveryMethod: sms})
	if response.GetError() == nil || protocol.codes.Load() != 0 {
		t.Fatal("transfer source accepted a registration operation")
	}
}

const sms = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS

type testProtocol struct {
	probes, codes, submits, closes atomic.Int32
	passwords                      atomic.Int32
	passwordResult                 *wacore.EngineRegisterResult
	password                       func(string)
	codeError                      error
	submitError                    error
	codeResult                     *wacore.EngineCodeResult
	submitResult                   *wacore.EngineRegisterResult
	block                          bool
	started                        chan struct{}
}

func (p *testProtocol) Probe(context.Context) wacore.EngineProbeResult {
	p.probes.Add(1)
	return wacore.EngineProbeResult{SupportedMethods: []waappv1.VerificationDeliveryMethod{sms, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD}}
}
func (p *testProtocol) RequestCode(ctx context.Context, _ waappv1.VerificationDeliveryMethod) wacore.EngineCodeResult {
	p.codes.Add(1)
	if p.block {
		close(p.started)
		<-ctx.Done()
		return wacore.EngineCodeResult{Err: ctx.Err()}
	}
	if p.codeError != nil {
		return wacore.EngineCodeResult{Err: p.codeError}
	}
	if p.codeResult != nil {
		return *p.codeResult
	}
	return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT, ExpectedCodeLength: 6}
}
func (p *testProtocol) SubmitCode(context.Context, waappv1.VerificationDeliveryMethod, string) wacore.EngineRegisterResult {
	p.submits.Add(1)
	if p.submitError != nil {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, OutcomeUnknown: true, Err: p.submitError}
	}
	if p.submitResult != nil {
		return *p.submitResult
	}
	return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, ServiceLoginID: "synthetic-login", ServiceAccountID: "synthetic-account"}
}
func (p *testProtocol) RefreshTransfer(context.Context) wacore.EngineAccountTransferChallengeResult {
	return wacore.EngineAccountTransferChallengeResult{}
}
func (p *testProtocol) SubmitPassword(_ context.Context, password string) wacore.EngineRegisterResult {
	p.passwords.Add(1)
	if p.password != nil {
		p.password(password)
	}
	if p.passwordResult != nil {
		return *p.passwordResult
	}
	return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, ServiceLoginID: "synthetic-login", ServiceAccountID: "synthetic-account"}
}
func (p *testProtocol) Material(string, string) (*waappv1.AcceptedAccountMaterial, error) {
	return &waappv1.AcceptedAccountMaterial{}, nil
}
func (p *testProtocol) Close() error { p.closes.Add(1); return nil }

type testAcceptor struct {
	calls     atomic.Int32
	loseReply bool
	record    *waappv1.AcceptAccountResponse
	protocol  *testProtocol
	requestID string
}

func (a *testAcceptor) AcceptAccount(_ context.Context, req *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error) {
	if a.protocol.closes.Load() != 1 {
		return nil, errors.New("registration protocol was not closed before handoff")
	}
	a.calls.Add(1)
	a.requestID = req.GetContext().GetRequestId()
	a.record = &waappv1.AcceptAccountResponse{Account: &waappv1.WAAccount{WaAccountId: "synthetic-account"}}
	if a.loseReply {
		return nil, errors.New("acceptance reply lost")
	}
	return a.record, nil
}
func (a *testAcceptor) GetAccountAcceptance(_ context.Context, req *waappv1.GetAccountAcceptanceRequest) (*waappv1.AcceptAccountResponse, error) {
	if req.GetAcceptanceRequestId() != a.requestID {
		return nil, errors.New("acceptance request ID changed")
	}
	return a.record, nil
}

func testService(t *testing.T) (*Service, *testProtocol, *testAcceptor) {
	t.Helper()
	protocol := &testProtocol{}
	acceptor := &testAcceptor{protocol: protocol}
	factory := func(context.Context, *waappv1.BeginAccountAddRequest) (Protocol, error) { return protocol, nil }
	validate := func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		if protocol.closes.Load() != 1 {
			return nil, errors.New("registration material was not released")
		}
		return material, nil
	}
	service := New(t.Context(), factory, validate, acceptor, nil, nil)
	t.Cleanup(service.closeAll)
	return service, protocol, acceptor
}
func requestContext(id string) *waappv1.RequestContext { return &waappv1.RequestContext{RequestId: id} }
func beginRequest(transfer bool) *waappv1.BeginAccountAddRequest {
	source := &waappv1.AccountAddSource{Method: &waappv1.AccountAddSource_Registration{Registration: &waappv1.RegistrationAccountSource{}}}
	if transfer {
		source.Method = &waappv1.AccountAddSource_Transfer{Transfer: &waappv1.TransferAccountSource{}}
	}
	return &waappv1.BeginAccountAddRequest{Context: requestContext("begin"), Phone: &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123"}, Source: source}
}

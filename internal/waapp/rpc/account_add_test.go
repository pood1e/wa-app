package rpc_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	adding "github.com/pood1e/wa-app/internal/waapp/app/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestIndependentServicesDeliverAccount(t *testing.T) {
	testIndependentServicesDeliverAccount(t, false)
}

func TestIndependentServicesDeliverUncertainAccount(t *testing.T) {
	testIndependentServicesDeliverAccount(t, true)
}

func testIndependentServicesDeliverAccount(t *testing.T, uncertain bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	repo, err := store.NewSQLiteStoreFile(ctx, filepath.Join(t.TempDir(), "accounts.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	runtime := account.New(repo, nil, nil, nil, nil, nil)
	token := "synthetic-service-token"
	runtimeAddress := serveFixture(t, token, func(server *grpc.Server) { rpc.RegisterRuntime(server, runtime) })
	if err := rpc.CheckHealth(ctx, runtimeAddress); err != nil {
		t.Fatal(err)
	}
	unauthorized, err := rpc.Dial(runtimeAddress, "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	defer unauthorized.Close()
	_, err = waappv1.NewWaProfileServiceClient(unauthorized).ListWAAccounts(ctx, &waappv1.ListWAAccountsRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatal("runtime API accepted missing service authentication")
	}
	runtimeConnection, err := rpc.Dial(runtimeAddress, token)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeConnection.Close()
	var closed atomic.Bool
	factory := func(_ context.Context, req *waappv1.BeginAccountAddRequest) (adding.Protocol, error) {
		native, err := onboarding.NewAndroidFlow(t.Context(), req.GetPhone(), protocoltest.AndroidBuild(t), protocoltest.AndroidClient(), "", onboarding.AndroidDependencies{})
		if err != nil {
			return nil, err
		}
		return &fixtureProtocol{AndroidFlow: native, closed: &closed, uncertain: uncertain}, nil
	}
	var validations atomic.Int32
	validate := func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		validations.Add(1)
		if closed.Load() == uncertain {
			t.Error("registration protocol was closed before confirmation or retained after confirmed registration")
		}
		return material, nil
	}
	addApplication := adding.New(ctx, factory, validate, rpc.NewAcceptanceClient(runtimeConnection), nil, nil)
	addAddress := serveFixture(t, token, func(server *grpc.Server) { rpc.RegisterAccountAdd(server, addApplication) })
	addConnection, err := rpc.Dial(addAddress, token)
	if err != nil {
		t.Fatal(err)
	}
	defer addConnection.Close()
	client := waappv1.NewWaAccountAddServiceClient(addConnection)
	response, err := client.BeginAccountAdd(ctx, &waappv1.BeginAccountAddRequest{Context: &waappv1.RequestContext{RequestId: "begin-fixture"},
		Phone:  &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"},
		Source: &waappv1.AccountAddSource{Method: &waappv1.AccountAddSource_Registration{Registration: &waappv1.RegistrationAccountSource{}}}})
	if err != nil || response.GetError() != nil {
		t.Fatal("begin flow failed")
	}
	id := response.GetFlow().GetFlowId()
	response, err = client.RequestAccountAddCode(ctx, &waappv1.RequestAccountAddCodeRequest{Context: &waappv1.RequestContext{RequestId: "code-fixture"}, FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS})
	if err != nil || response.GetError() != nil {
		t.Fatal("request code failed")
	}
	response, err = client.SubmitAccountAddCode(ctx, &waappv1.SubmitAccountAddCodeRequest{Context: &waappv1.RequestContext{RequestId: "submit-fixture"}, FlowId: id, Code: "000000"})
	if err != nil || response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		t.Fatal("authenticated account handoff failed")
	}
	receipt, err := rpc.NewAcceptanceClient(runtimeConnection).GetAccountAcceptance(ctx, &waappv1.GetAccountAcceptanceRequest{AcceptanceRequestId: "account-add/begin-fixture"})
	if err != nil || receipt.GetError() != nil || receipt.GetAccount().GetWaAccountId() != response.GetFlow().GetAccount().GetWaAccountId() {
		t.Fatal("durable acceptance receipt was not available")
	}
	accounts, err := waappv1.NewWaProfileServiceClient(runtimeConnection).ListWAAccounts(ctx, &waappv1.ListWAAccountsRequest{})
	if err != nil || len(accounts.GetAccounts()) != 1 {
		t.Fatal("runtime did not retain the added account")
	}
	if validations.Load() != 1 || !closed.Load() {
		t.Fatal("account delivery repeated validation or retained the temporary protocol")
	}
}

func serveFixture(t *testing.T, token string, register func(*grpc.Server)) string {
	t.Helper()
	server, err := rpc.NewServer(token)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	register(server)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-done })
	return listener.Addr().String()
}

type fixtureProtocol struct {
	*onboarding.AndroidFlow
	closed    *atomic.Bool
	uncertain bool
}

func (p *fixtureProtocol) Probe(context.Context) wacore.EngineProbeResult {
	return wacore.EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE, SupportedMethods: []waappv1.VerificationDeliveryMethod{waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS}}
}
func (p *fixtureProtocol) RequestCode(context.Context, waappv1.VerificationDeliveryMethod) wacore.EngineCodeResult {
	return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT, ExpectedCodeLength: 6}
}
func (p *fixtureProtocol) SubmitCode(context.Context, waappv1.VerificationDeliveryMethod, string) wacore.EngineRegisterResult {
	if p.uncertain {
		return wacore.EngineRegisterResult{OutcomeUnknown: true, Err: errors.New("upstream HTTP 502")}
	}
	return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, ServiceLoginID: "12025550123", ServiceAccountID: "fixture-account"}
}
func (p *fixtureProtocol) Material(login, account string) (*waappv1.AcceptedAccountMaterial, error) {
	material, err := p.AndroidFlow.Material(login, account)
	if err == nil {
		material.Recovery.RegistrationJid = login + "@s.whatsapp.net"
	}
	return material, err
}
func (p *fixtureProtocol) Close() error { p.closed.Store(true); return p.AndroidFlow.Close() }

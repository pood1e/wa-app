package onboarding

import (
	"context"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

type proxyAcceptor struct {
	*testAcceptor
	proxy *waappv1.AccountProxy
}

func (a *proxyAcceptor) AcceptAccount(ctx context.Context, request *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error) {
	a.proxy = proto.CloneOf(request.GetMaterial().GetProxy())
	return a.testAcceptor.AcceptAccount(ctx, request)
}

func TestProxySurvivesAddingAndDelivery(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		for _, custom := range []bool{false, true} {
			t.Run(map[bool]string{true: "transfer", false: "registration"}[transfer]+map[bool]string{true: "/custom", false: "/saved"}[custom], func(t *testing.T) {
				service, protocol, acceptor := testService(t)
				selected := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP, Host: "proxy.example", Port: 8080, Username: "fixture", Password: &waappv1.SensitiveText{Value: "fixture-password"}}
				service.factory = func(context.Context, *waappv1.BeginAccountAddRequest) (Protocol, error) {
					return WithAccountProxy(protocol, selected), nil
				}
				originalValidate := service.validate
				service.validate = func(ctx context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
					if !proto.Equal(material.Proxy, selected) {
						t.Error("temporary login validation lost the selected proxy")
					}
					return originalValidate(ctx, proto.CloneOf(material))
				}
				captured := &proxyAcceptor{testAcceptor: acceptor}
				service.acceptor = captured
				request := beginRequest(transfer)
				if custom {
					request.Proxy = proto.CloneOf(selected)
				}
				response, _ := service.BeginAccountAdd(t.Context(), request)
				id := response.GetFlow().GetFlowId()
				repeated, _ := service.BeginAccountAdd(t.Context(), request)
				if repeated.GetError() != nil || repeated.GetFlow().GetFlowId() != id || protocol.probes.Load() != 1 {
					t.Fatal("proxy resolution broke begin idempotency")
				}
				changed := proto.CloneOf(request)
				changed.Proxy = proto.CloneOf(selected)
				changed.Proxy.Host = "different.example"
				conflict, _ := service.BeginAccountAdd(t.Context(), changed)
				if conflict.GetError() == nil {
					t.Fatal("request ID reused with another proxy")
				}
				method := sms
				if transfer {
					method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
				}
				response, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("proxy-code"), FlowId: id, DeliveryMethod: method})
				if response.GetError() != nil {
					t.Fatal("proxy flow code request failed")
				}
				response, _ = service.SubmitAccountAddCode(t.Context(), &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("proxy-submit"), FlowId: id, Code: "123456"})
				if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || !proto.Equal(captured.proxy, selected) {
					t.Fatal("runtime acceptance lost the selected proxy")
				}
				current := service.lookup(id)
				if current.request.GetProxy() != nil || current.material != nil || current.protocol != nil {
					t.Fatal("completed flow retained proxy credentials")
				}
			})
		}
	}
}

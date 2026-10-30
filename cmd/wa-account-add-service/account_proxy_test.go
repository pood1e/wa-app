package main

import (
	"context"
	"errors"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type proxyRuntime struct {
	waappv1.WaAccountAcceptanceServiceClient
	value *waappv1.AccountProxy
	err   error
	calls int
}

func (r *proxyRuntime) ResolveAccountAddProxy(_ context.Context, request *waappv1.ResolveAccountAddProxyRequest, _ ...grpc.CallOption) (*waappv1.AccountProxyResponse, error) {
	r.calls++
	if request.GetPhone().GetE164Number() == "" {
		return nil, errors.New("missing phone")
	}
	return &waappv1.AccountProxyResponse{Proxy: r.value}, r.err
}

func TestResolveAccountProxy(t *testing.T) {
	saved := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5, Host: "saved.example", Port: 1080}
	override := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP, Host: "override.example", Port: 8080}
	for _, tc := range []struct {
		name           string
		request, saved *waappv1.AccountProxy
		failed         bool
		address        string
		calls          int
	}{
		{"global", nil, nil, false, "http://default.example:8080", 1},
		{"saved", nil, saved, false, "socks5://saved.example:1080", 1},
		{"override", override, saved, false, "http://override.example:8080", 0},
		{"lookup_failed", nil, saved, true, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &proxyRuntime{value: tc.saved}
			if tc.failed {
				runtime.err = errors.New("lookup failed")
			}
			request := &waappv1.BeginAccountAddRequest{Phone: &waappv1.PhoneTarget{E164Number: "+12025550123"}, Proxy: tc.request}
			before := proto.CloneOf(request)
			_, address, err := resolveAccountProxy(t.Context(), runtime, request, "http://default.example:8080")
			if (err != nil) != tc.failed || address != tc.address || runtime.calls != tc.calls || !proto.Equal(before, request) {
				t.Fatal("proxy selection changed request identity or used an incorrect fallback")
			}
		})
	}
}

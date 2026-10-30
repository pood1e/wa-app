package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waAccountAcceptanceServiceServer struct {
	waappv1.UnimplementedWaAccountAcceptanceServiceServer
	application *account.Application
}

func (s *waAccountAcceptanceServiceServer) AcceptAccount(ctx context.Context, req *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error) {
	return s.application.AcceptAccount(ctx, req)
}
func (s *waAccountAcceptanceServiceServer) GetAccountAcceptance(ctx context.Context, req *waappv1.GetAccountAcceptanceRequest) (*waappv1.AcceptAccountResponse, error) {
	return s.application.GetAccountAcceptance(ctx, req)
}

func (s *waAccountAcceptanceServiceServer) ResolveAccountAddProxy(ctx context.Context, req *waappv1.ResolveAccountAddProxyRequest) (*waappv1.AccountProxyResponse, error) {
	return s.application.ResolveAccountAddProxy(ctx, req)
}

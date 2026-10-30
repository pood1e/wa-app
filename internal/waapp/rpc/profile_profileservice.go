package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waProfileServiceServer struct {
	waappv1.UnimplementedWaProfileServiceServer
	application *account.Application
}

func (s *waProfileServiceServer) GetWAAccount(ctx context.Context, req *waappv1.GetWAAccountRequest) (*waappv1.GetWAAccountResponse, error) {
	return s.application.GetWAAccount(ctx, req)
}
func (s *waProfileServiceServer) ListWAAccounts(ctx context.Context, req *waappv1.ListWAAccountsRequest) (*waappv1.ListWAAccountsResponse, error) {
	return s.application.ListWAAccounts(ctx, req)
}
func (s *waProfileServiceServer) DeleteWAAccount(ctx context.Context, req *waappv1.DeleteWAAccountRequest) (*waappv1.DeleteWAAccountResponse, error) {
	return s.application.DeleteWAAccount(ctx, req)
}

func (s *waProfileServiceServer) LogoutWAAccount(ctx context.Context, req *waappv1.LogoutWAAccountRequest) (*waappv1.LogoutWAAccountResponse, error) {
	return s.application.LogoutWAAccount(ctx, req)
}

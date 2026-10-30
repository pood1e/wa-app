package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waLoginServiceServer struct {
	waappv1.UnimplementedWaLoginServiceServer
	application *account.Application
}

func (s *waLoginServiceServer) GetLoginState(ctx context.Context, req *waappv1.GetLoginStateRequest) (*waappv1.GetLoginStateResponse, error) {
	return s.application.GetLoginState(ctx, req)
}
func (s *waLoginServiceServer) GetActiveLoginState(ctx context.Context, req *waappv1.GetActiveLoginStateRequest) (*waappv1.GetActiveLoginStateResponse, error) {
	return s.application.GetActiveLoginState(ctx, req)
}
func (s *waLoginServiceServer) CheckLoginState(ctx context.Context, req *waappv1.CheckLoginStateRequest) (*waappv1.CheckLoginStateResponse, error) {
	return s.application.CheckLoginState(ctx, req)
}

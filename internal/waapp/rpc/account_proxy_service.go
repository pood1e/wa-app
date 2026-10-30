package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type accountProxyServiceServer struct {
	waappv1.UnimplementedWaAccountProxyServiceServer
	application *account.Application
}

func (s *accountProxyServiceServer) GetAccountProxy(ctx context.Context, req *waappv1.GetAccountProxyRequest) (*waappv1.AccountProxyResponse, error) {
	return s.application.GetAccountProxy(ctx, req)
}

func (s *accountProxyServiceServer) SetAccountProxy(ctx context.Context, req *waappv1.SetAccountProxyRequest) (*waappv1.AccountProxyResponse, error) {
	return s.application.SetAccountProxy(ctx, req)
}

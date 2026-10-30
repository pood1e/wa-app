package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	addapp "github.com/pood1e/wa-app/internal/waapp/app/onboarding"
)

type waAccountAddServiceServer struct {
	waappv1.UnimplementedWaAccountAddServiceServer
	application *addapp.Service
}

func (s *waAccountAddServiceServer) BeginAccountAdd(ctx context.Context, req *waappv1.BeginAccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.BeginAccountAdd(ctx, req)
}
func (s *waAccountAddServiceServer) GetAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.GetAccountAdd(ctx, req)
}
func (s *waAccountAddServiceServer) RequestAccountAddCode(ctx context.Context, req *waappv1.RequestAccountAddCodeRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.RequestAccountAddCode(ctx, req)
}
func (s *waAccountAddServiceServer) SubmitAccountAddCode(ctx context.Context, req *waappv1.SubmitAccountAddCodeRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.SubmitAccountAddCode(ctx, req)
}
func (s *waAccountAddServiceServer) SubmitAccountAddPassword(ctx context.Context, req *waappv1.SubmitAccountAddPasswordRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.SubmitAccountAddPassword(ctx, req)
}
func (s *waAccountAddServiceServer) RefreshAccountAddTransfer(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.RefreshAccountAddTransfer(ctx, req)
}
func (s *waAccountAddServiceServer) DeliverAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.DeliverAccountAdd(ctx, req)
}
func (s *waAccountAddServiceServer) CancelAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.application.CancelAccountAdd(ctx, req)
}

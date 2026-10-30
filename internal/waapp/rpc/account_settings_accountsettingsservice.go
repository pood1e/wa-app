package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waAccountSettingsServiceServer struct {
	waappv1.UnimplementedWaAccountSettingsServiceServer
	application *account.Application
}

func (s *waAccountSettingsServiceServer) GetTwoFactorAuthStatus(ctx context.Context, req *waappv1.GetTwoFactorAuthStatusRequest) (*waappv1.GetTwoFactorAuthStatusResponse, error) {
	return s.application.GetTwoFactorAuthStatus(ctx, req)
}
func (s *waAccountSettingsServiceServer) SetTwoFactorAuthSettings(ctx context.Context, req *waappv1.SetTwoFactorAuthSettingsRequest) (*waappv1.SetTwoFactorAuthSettingsResponse, error) {
	return s.application.SetTwoFactorAuthSettings(ctx, req)
}
func (s *waAccountSettingsServiceServer) SetAccountEmail(ctx context.Context, req *waappv1.SetAccountEmailRequest) (*waappv1.SetAccountEmailResponse, error) {
	return s.application.SetAccountEmail(ctx, req)
}
func (s *waAccountSettingsServiceServer) RequestAccountEmailOtp(ctx context.Context, req *waappv1.RequestAccountEmailOtpRequest) (*waappv1.RequestAccountEmailOtpResponse, error) {
	return s.application.RequestAccountEmailOtp(ctx, req)
}
func (s *waAccountSettingsServiceServer) VerifyAccountEmailOtp(ctx context.Context, req *waappv1.VerifyAccountEmailOtpRequest) (*waappv1.VerifyAccountEmailOtpResponse, error) {
	return s.application.VerifyAccountEmailOtp(ctx, req)
}
func (s *waAccountSettingsServiceServer) SetAccountProfileName(ctx context.Context, req *waappv1.SetAccountProfileNameRequest) (*waappv1.SetAccountProfileNameResponse, error) {
	return s.application.SetAccountProfileName(ctx, req)
}
func (s *waAccountSettingsServiceServer) GetAccountProfilePicture(ctx context.Context, req *waappv1.GetAccountProfilePictureRequest) (*waappv1.GetAccountProfilePictureResponse, error) {
	return s.application.GetAccountProfilePicture(ctx, req)
}
func (s *waAccountSettingsServiceServer) SetAccountProfilePicture(ctx context.Context, req *waappv1.SetAccountProfilePictureRequest) (*waappv1.SetAccountProfilePictureResponse, error) {
	return s.application.SetAccountProfilePicture(ctx, req)
}
func (s *waAccountSettingsServiceServer) RemoveAccountProfilePicture(ctx context.Context, req *waappv1.RemoveAccountProfilePictureRequest) (*waappv1.RemoveAccountProfilePictureResponse, error) {
	return s.application.RemoveAccountProfilePicture(ctx, req)
}

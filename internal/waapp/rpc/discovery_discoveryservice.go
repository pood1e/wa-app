package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waDiscoveryServiceServer struct {
	waappv1.UnimplementedWaDiscoveryServiceServer
	application *account.Application
}

func (s *waDiscoveryServiceServer) RegisterAppArtifact(ctx context.Context, req *waappv1.RegisterAppArtifactRequest) (*waappv1.RegisterAppArtifactResponse, error) {
	return s.application.RegisterAppArtifact(ctx, req)
}
func (s *waDiscoveryServiceServer) RecordProtocolProfile(ctx context.Context, req *waappv1.RecordProtocolProfileRequest) (*waappv1.RecordProtocolProfileResponse, error) {
	return s.application.RecordProtocolProfile(ctx, req)
}
func (s *waDiscoveryServiceServer) GetProtocolProfile(ctx context.Context, req *waappv1.GetProtocolProfileRequest) (*waappv1.GetProtocolProfileResponse, error) {
	return s.application.GetProtocolProfile(ctx, req)
}

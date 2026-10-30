package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waExtractionServiceServer struct {
	waappv1.UnimplementedWaExtractionServiceServer
	application *account.Application
}

func (s *waExtractionServiceServer) DecryptMessage(ctx context.Context, req *waappv1.DecryptMessageRequest) (*waappv1.DecryptMessageResponse, error) {
	return s.application.DecryptMessage(ctx, req)
}
func (s *waExtractionServiceServer) ExtractCandidates(ctx context.Context, req *waappv1.ExtractCandidatesRequest) (*waappv1.ExtractCandidatesResponse, error) {
	return s.application.ExtractCandidates(ctx, req)
}
func (s *waExtractionServiceServer) ListAccountOtpMessages(ctx context.Context, req *waappv1.ListAccountOtpMessagesRequest) (*waappv1.ListAccountOtpMessagesResponse, error) {
	return s.application.ListAccountOtpMessages(ctx, req)
}

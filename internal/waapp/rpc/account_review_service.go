package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type accountReviewServiceServer struct {
	waappv1.UnimplementedWaAccountReviewServiceServer
	application *account.Application
}

func (s *accountReviewServiceServer) GetAccountReview(ctx context.Context, req *waappv1.GetAccountReviewRequest) (*waappv1.AccountReviewResponse, error) {
	return s.application.GetAccountReview(ctx, req)
}

func (s *accountReviewServiceServer) SubmitAccountReview(ctx context.Context, req *waappv1.SubmitAccountReviewRequest) (*waappv1.AccountReviewResponse, error) {
	return s.application.SubmitAccountReview(ctx, req)
}

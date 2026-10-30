package account

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

// AccountReviewProtocol operates on a private client profile without an active login.
type AccountReviewProtocol interface {
	GetAccountReview(context.Context, string, bool) (*protocolv1.AccountReview, error)
	SubmitAccountReview(context.Context, string, string) (*protocolv1.AccountReview, error)
}

type accountReviewHandler struct{ *serverCore }

func (s *accountReviewHandler) GetAccountReview(ctx context.Context, req *waappv1.GetAccountReviewRequest) (*waappv1.AccountReviewResponse, error) {
	return s.reviewOperation(ctx, req.GetWaAccountId(), func(ctx context.Context, protocol AccountReviewProtocol, profileID string) (*protocolv1.AccountReview, error) {
		return protocol.GetAccountReview(ctx, profileID, req.GetRemoteRefresh())
	})
}

func (s *accountReviewHandler) SubmitAccountReview(ctx context.Context, req *waappv1.SubmitAccountReviewRequest) (*waappv1.AccountReviewResponse, error) {
	description := strings.TrimSpace(req.GetDescription().GetValue())
	if description == "" || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 1000 {
		return reviewResponse(nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "review description must contain 1 to 1000 characters", false)), nil
	}
	return s.reviewOperation(ctx, req.GetWaAccountId(), func(ctx context.Context, protocol AccountReviewProtocol, profileID string) (*protocolv1.AccountReview, error) {
		return protocol.SubmitAccountReview(ctx, profileID, description)
	})
}

func (s *accountReviewHandler) reviewOperation(ctx context.Context, accountID string, operation func(context.Context, AccountReviewProtocol, string) (*protocolv1.AccountReview, error)) (*waappv1.AccountReviewResponse, error) {
	id, err := wamodel.RequireWAAccountID(accountID)
	if err != nil {
		return reviewResponse(nil, err), nil
	}
	if _, err := s.GetWAAccountRecord(ctx, id); err != nil {
		return reviewResponse(nil, err), nil
	}
	login, err := s.store.GetLatestAccountLoginState(ctx, id)
	if err != nil {
		return reviewResponse(nil, err), nil
	}
	if login.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID || login.GetLastError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED {
		return reviewResponse(&protocolv1.AccountReview{Status: protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_UNAVAILABLE}, nil), nil
	}
	protocol, ok := s.runner.(AccountReviewProtocol)
	if !ok || s.runtime == nil {
		return reviewResponse(nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "account review protocol is unavailable", false)), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	key, holder := "account-review/"+id, s.ids.NewID("review_")
	granted, err := s.runtime.ClaimLease(ctx, key, holder, 2*time.Minute)
	if err != nil {
		return reviewResponse(nil, err), nil
	}
	if !granted {
		return reviewResponse(nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "another account review operation is in progress", true)), nil
	}
	defer func() {
		releaseCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer finish()
		_ = s.runtime.ReleaseLease(releaseCtx, key, holder)
	}()
	decision, err := operation(ctx, protocol, login.GetClientProfileId())
	return reviewResponse(decision, err), nil
}

func reviewResponse(review *protocolv1.AccountReview, err error) *waappv1.AccountReviewResponse {
	return &waappv1.AccountReviewResponse{Review: review, Error: shared.ToProtoError(err)}
}

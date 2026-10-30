package communication

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/appeal"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// GetAccountReview reads the persisted decision, optionally querying the server.
func (e *Engine) GetAccountReview(ctx context.Context, profileID string, refresh bool) (*protocolv1.AccountReview, error) {
	current, err := e.loadState(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if !refresh {
		return cachedAccountReview(current), nil
	}
	if current.AccountReview.GetToken() == "" {
		if err := e.captureReviewAuthorization(ctx, profileID, &current); err != nil {
			return cachedAccountReview(current), err
		}
	}
	if current.AccountReview.GetToken() == "" {
		return e.saveAccountReview(ctx, profileID, current, protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_UNAVAILABLE)
	}
	httpClient, finish, err := e.HTTPForProfile(ctx, profileID)
	if err != nil {
		return cachedAccountReview(current), err
	}
	defer finish()
	client, err := appeal.New(httpClient.Client)
	if err != nil {
		return cachedAccountReview(current), err
	}
	status, err := client.Status(ctx, current.AccountReview.GetToken(), current.Client)
	if err != nil {
		return cachedAccountReview(current), err
	}
	return e.saveAccountReview(ctx, profileID, current, status)
}

// SubmitAccountReview checks the current server decision before one submission.
func (e *Engine) SubmitAccountReview(ctx context.Context, profileID, description string) (*protocolv1.AccountReview, error) {
	decision, err := e.GetAccountReview(ctx, profileID, true)
	if err != nil || decision.GetStatus() != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_NOT_REQUESTED {
		return decision, err
	}
	current, err := e.loadState(ctx, profileID)
	if err != nil {
		return decision, err
	}
	httpClient, finish, err := e.HTTPForProfile(ctx, profileID)
	if err != nil {
		return decision, err
	}
	defer finish()
	client, err := appeal.New(httpClient.Client)
	if err != nil {
		return decision, err
	}
	unknown, err := e.saveAccountReview(ctx, profileID, current, protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN)
	if err != nil {
		return decision, err
	}
	status, err := client.Submit(ctx, current.AccountReview.GetToken(), description, current.Client)
	if err != nil {
		return unknown, err
	}
	return e.saveAccountReview(ctx, profileID, current, status)
}

func (e *engineCore) captureReviewAuthorization(ctx context.Context, profileID string, current *state.NativeState) error {
	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	proxyURL, err := e.proxyURL(ctx, profileID)
	if err != nil {
		return err
	}
	cfg := ChatdConfigForState(proxyURL, *current, 25*time.Second)
	cfg.MaxEndpoints = 1
	client := NewChatdClient(cfg)
	_, err = client.CheckLoginState(probeCtx, *current, wacore.EngineLoginCheckInput{}, current.Client.GetAppVersion())
	var rejection *loginRejection
	if !errors.As(err, &rejection) {
		if err == nil || IsAuthenticationFailure(err) {
			return nil
		}
		return ChatdReceiveError(err)
	}
	if current.AccountReview != nil {
		rejection.review.Review = current.AccountReview.GetReview()
	}
	current.AccountReview = rejection.review
	return e.SaveState(ctx, profileID, *current)
}

func (e *engineCore) saveAccountReview(ctx context.Context, profileID string, current state.NativeState, status protocolv1.AccountReviewStatus) (*protocolv1.AccountReview, error) {
	decision := &protocolv1.AccountReview{Status: status, CheckedAt: timestamppb.New(e.clock.Now())}
	if current.AccountReview == nil {
		current.AccountReview = new(protocolv1.AccountReviewState)
	}
	current.AccountReview.Review = decision
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.SaveState(saveCtx, profileID, current); err != nil {
		return decision, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "account review state could not be saved", false)
	}
	return decision, nil
}

func cachedAccountReview(current state.NativeState) *protocolv1.AccountReview {
	if decision := current.AccountReview.GetReview(); decision != nil {
		return proto.Clone(decision).(*protocolv1.AccountReview)
	}
	return new(protocolv1.AccountReview)
}

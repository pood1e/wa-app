package communication

import (
	"context"
	"errors"
	"fmt"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type loginRejection struct {
	cause  error
	review *protocolv1.AccountReviewState
}

func (e *loginRejection) Error() string { return e.cause.Error() }
func (e *loginRejection) Unwrap() error { return e.cause }

func chatdAuthenticationFailure(node wire.ChatdNode) error {
	if node.Tag != "failure" {
		return nil
	}
	switch reason := node.Attrs["reason"]; reason {
	case "401", "402", "403", "405", "406", "409", "413", "414":
		cause := shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED,
			fmt.Sprintf("WA server rejected login (code %s)", reason), false)
		if reason == "403" && node.Attrs["appeal_token"] != "" && len(node.Attrs["appeal_token"]) <= 16<<10 {
			return &loginRejection{cause: cause, review: &protocolv1.AccountReviewState{Token: node.Attrs["appeal_token"]}}
		}
		return cause
	default:
		return nil
	}
}

func (e *engineCore) saveLoginRejection(ctx context.Context, profileID string, current state.NativeState, cause error) error {
	var rejection *loginRejection
	if !errors.As(cause, &rejection) {
		return cause
	}
	if current.AccountReview != nil {
		rejection.review.Review = current.AccountReview.GetReview()
	}
	current.AccountReview = rejection.review
	return errors.Join(cause, e.SaveState(ctx, profileID, current))
}

func IsAuthenticationFailure(err error) bool {
	return err != nil && shared.ToProtoError(err).GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED
}

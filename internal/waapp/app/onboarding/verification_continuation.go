package onboarding

import (
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *Service) resumeVerification(current *flow, result wacore.EngineRegisterResult) {
	current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY
	verification := current.snapshot.GetVerification()
	verification.Status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED
	verification.LastError = shared.ToProtoError(result.Err)
	verification.ExpiresAt = nil
	now := s.clock.Now()
	retry := result.RetryAfter
	if next := result.NextVerification; next != nil {
		retry = max(retry, next.GetCooldown().AsDuration())
	}
	if next := now.Add(retry); next.After(current.nextCodeAt) {
		current.nextCodeAt = next
	}
	verification.RetryAfter = shared.DurationToProto(current.nextCodeAt.Sub(verification.GetRequestedAt().AsTime()))
	if next := result.NextVerification; next != nil {
		current.snapshot.RequiredVerificationMethod = next.GetDeliveryMethod()
		verification.MethodStatuses = []*waappv1.VerificationMethodStatus{{
			DeliveryMethod: next.GetDeliveryMethod(), Available: !now.Before(current.nextCodeAt), Cooldown: verification.GetRetryAfter(),
		}}
	}
}

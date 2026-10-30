package onboarding

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *Service) SubmitAccountAddPassword(ctx context.Context, req *waappv1.SubmitAccountAddPasswordRequest) (*waappv1.AccountAddResponse, error) {
	return s.operate(ctx, req.GetFlowId(), req, func(ctx context.Context, current *flow) error {
		status := current.snapshot.GetStatus()
		challenge := current.snapshot.GetPasswordChallenge()
		if challenge == nil || (status != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_PASSWORD && status != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN) {
			return conflict("account adding flow is not waiting for a password")
		}
		if next := challenge.GetNextAttemptAt(); next != nil && s.clock.Now().Before(next.AsTime()) {
			return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "two-step verification is cooling down", true)
		}
		password := req.GetPassword().GetValue()
		if password == "" || len(password) > 256 {
			return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "two-step verification password is required", false)
		}
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN
		result := current.protocol.SubmitPassword(ctx, password)
		if result.PasswordChallenge != nil {
			current.snapshot.PasswordChallenge = result.PasswordChallenge
		}
		if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
			return s.completeRegistration(ctx, current, result)
		}
		if result.OutcomeUnknown {
			return s.recoverUncertainRegistration(ctx, current, result.Err)
		}
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_PASSWORD
		if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE {
			current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY
			current.snapshot.PasswordChallenge = nil
			current.snapshot.Verification.Status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED
			current.snapshot.Verification.ExpiresAt = nil
			current.snapshot.Verification.LastError = shared.ToProtoError(result.Err)
			if next := s.clock.Now().Add(result.RetryAfter); next.After(current.nextCodeAt) {
				current.nextCodeAt = next
			}
			current.snapshot.Verification.RetryAfter = shared.DurationToProto(current.nextCodeAt.Sub(current.snapshot.Verification.GetRequestedAt().AsTime()))
		}
		if result.Err == nil && result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD {
			return conflict("password verification has not completed")
		}
		return result.Err
	})
}

func (s *Service) completeRegistration(ctx context.Context, current *flow, result wacore.EngineRegisterResult) error {
	material, err := current.protocol.Material(result.ServiceLoginID, result.ServiceAccountID)
	if err != nil {
		return err
	}
	current.material = material
	current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY_TO_DELIVER
	current.snapshot.TransferChallenge = nil
	current.snapshot.PasswordChallenge = nil
	return s.deliver(ctx, current)
}

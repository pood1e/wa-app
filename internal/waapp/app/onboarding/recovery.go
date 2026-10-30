package onboarding

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *Service) recoverUncertainRegistration(ctx context.Context, current *flow, registrationErr error) error {
	if registrationErr == nil {
		registrationErr = conflict("account registration outcome has not been confirmed")
	}
	if ctx.Err() != nil {
		return registrationErr
	}
	if err := s.recoverRegistration(ctx, current); err != nil {
		return registrationErr
	}
	return s.deliver(ctx, current)
}

func (s *Service) recoverRegistration(ctx context.Context, current *flow) error {
	verification := current.snapshot.GetVerification().GetStatus()
	if current.protocol == nil || (verification != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT && verification != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING) {
		return conflict("registration has not been submitted after confirmed verification")
	}
	login := shared.DigitsOnly(current.snapshot.GetPhone().GetE164Number())
	material, err := current.protocol.Material(login, login)
	if err != nil {
		return err
	}
	// Authentication proves the retained keys were accepted; no registration request is sent.
	validated, err := s.validateRegistration(ctx, material)
	if err != nil {
		return err
	}
	current.material, current.validated = validated, true
	current.snapshot.TransferChallenge = nil
	current.snapshot.PasswordChallenge = nil
	current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY_TO_DELIVER
	return nil
}

package onboarding

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *Service) DeliverAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.operate(ctx, req.GetFlowId(), req, func(ctx context.Context, current *flow) error {
		status := current.snapshot.GetStatus()
		transfer := status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE && current.snapshot.GetVerification().GetDeliveryMethod() == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
		if status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || transfer {
			if err := s.recoverRegistration(ctx, current); err != nil {
				return err
			}
		}
		return s.deliver(ctx, current)
	})
}

func (s *Service) deliver(ctx context.Context, current *flow) error {
	if current.snapshot.GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY_TO_DELIVER || current.material == nil {
		return conflict("account is not ready for delivery")
	}
	if current.protocol != nil {
		if err := current.protocol.Close(); err != nil {
			return err
		}
		current.protocol = nil
	}
	if !current.validated {
		material, err := s.validateRegistration(ctx, current.material)
		if err != nil {
			return err
		}
		current.material = material
		current.validated = true
	}
	if current.attemptedDelivery {
		response, err := s.acceptor.GetAccountAcceptance(ctx, &waappv1.GetAccountAcceptanceRequest{AcceptanceRequestId: current.acceptanceID})
		if err != nil {
			return err
		}
		if response.GetAccount() != nil && response.GetError() == nil {
			return current.accept(response.GetAccount())
		}
		if response.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND {
			return shared.ErrorFromProto(response.GetError())
		}
	}
	current.attemptedDelivery = true
	response, err := s.acceptor.AcceptAccount(ctx, &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: current.acceptanceID}, Material: current.material})
	if err != nil {
		return err
	}
	if response.GetError() != nil {
		return shared.ErrorFromProto(response.GetError())
	}
	if response.GetAccount() == nil {
		return conflict("account runtime did not acknowledge acceptance")
	}
	return current.accept(response.GetAccount())
}

func (f *flow) accept(account *waappv1.WAAccount) error {
	f.snapshot.Account = account
	f.snapshot.LastError = nil
	f.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED)
	return nil
}

func methodAvailable(snapshot *waappv1.AccountAddFlow, method waappv1.VerificationDeliveryMethod, now time.Time) bool {
	statuses := snapshot.GetProbe().GetMethodStatuses()
	observed := snapshot.GetProbe().GetProbedAt().AsTime()
	if len(snapshot.GetVerification().GetMethodStatuses()) > 0 {
		statuses = snapshot.GetVerification().GetMethodStatuses()
		observed = snapshot.GetVerification().GetRequestedAt().AsTime()
	}
	for _, status := range statuses {
		if status.GetDeliveryMethod() != method {
			continue
		}
		wait := shared.DurationFromProto(status.GetCooldown())
		return (status.GetAvailable() || wait > 0) && !now.Before(observed.Add(wait))
	}
	for _, supported := range snapshot.GetProbe().GetSupportedMethods() {
		if supported == method {
			return true
		}
	}
	return false
}

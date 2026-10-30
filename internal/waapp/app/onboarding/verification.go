package onboarding

import (
	"context"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *Service) RequestAccountAddCode(ctx context.Context, req *waappv1.RequestAccountAddCodeRequest) (*waappv1.AccountAddResponse, error) {
	return s.operate(ctx, req.GetFlowId(), req, func(ctx context.Context, current *flow) error {
		if current.snapshot.GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY {
			return conflict("account adding flow cannot request a code in its current state")
		}
		method := req.GetDeliveryMethod()
		if err := allowedMethod(current.request, method); err != nil {
			return err
		}
		if s.clock.Now().Before(current.nextCodeAt) {
			return conflict("verification code request is cooling down")
		}
		if !methodAvailable(current.snapshot, method, s.clock.Now()) {
			return conflict("verification method is unavailable or cooling down")
		}
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN
		result := current.protocol.RequestCode(ctx, method)
		requested := s.clock.Now()
		current.nextCodeAt = requested.Add(result.RetryAfter)
		record := &waappv1.VerificationCodeRequestRecord{VerificationRequestId: s.ids.NewID("wavrf_"), DeliveryMethod: method, Status: result.Status,
			ExpectedCodeLength: result.ExpectedCodeLength, RequestedAt: timestamppb.New(requested), ExpiresAt: shared.ProtoTimestamp(result.ExpiresAt),
			RetryAfter: shared.DurationToProto(result.RetryAfter), MethodStatuses: methodStatuses(result.MethodStatuses), LastError: shared.ToProtoError(result.Err)}
		current.snapshot.Verification = record
		current.snapshot.TransferChallenge = result.AccountTransferChallenge
		if current.snapshot.TransferChallenge != nil {
			current.snapshot.TransferChallenge.VerificationRequestId = record.GetVerificationRequestId()
		}
		if result.Err != nil {
			if result.RawStatus != "" || result.RawReason != "" {
				current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY
			}
			return result.Err
		}
		if result.Status != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT && result.Status != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING {
			current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY
			return conflict("verification code request was rejected")
		}
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE
		return nil
	})
}

func (s *Service) SubmitAccountAddCode(ctx context.Context, req *waappv1.SubmitAccountAddCodeRequest) (*waappv1.AccountAddResponse, error) {
	return s.operate(ctx, req.GetFlowId(), req, func(ctx context.Context, current *flow) error {
		status := current.snapshot.GetStatus()
		verification := current.snapshot.GetVerification()
		confirmed := verification.GetStatus() == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT ||
			verification.GetStatus() == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING
		canSubmit := confirmed && (status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE || status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN)
		if !canSubmit {
			return conflict("account adding flow is not waiting for a code")
		}
		method := current.snapshot.GetVerification().GetDeliveryMethod()
		if method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
			code := strings.TrimSpace(req.GetCode())
			length := int(current.snapshot.GetVerification().GetExpectedCodeLength())
			if length == 0 {
				length = 6
			}
			if len(code) != length || shared.DigitsOnly(code) != code {
				return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification code has invalid length or characters", false)
			}
		}
		current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN
		result := current.protocol.SubmitCode(ctx, method, req.GetCode())
		if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD {
			current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_PASSWORD
			current.snapshot.PasswordChallenge = result.PasswordChallenge
			if current.snapshot.PasswordChallenge == nil {
				current.snapshot.PasswordChallenge = &waappv1.AccountAddPasswordChallenge{}
			}
			return result.Err
		}
		if result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
			if result.OutcomeUnknown {
				return s.recoverUncertainRegistration(ctx, current, result.Err)
			}
			if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE {
				current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY
				verification.Status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED
				verification.LastError = shared.ToProtoError(result.Err)
				verification.ExpiresAt = nil
				if next := s.clock.Now().Add(result.RetryAfter); next.After(current.nextCodeAt) {
					current.nextCodeAt = next
				}
				verification.RetryAfter = shared.DurationToProto(current.nextCodeAt.Sub(verification.GetRequestedAt().AsTime()))
			} else if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED || result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_SUBMITTED {
				current.snapshot.Status = waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE
			}
			if result.Err != nil {
				return result.Err
			}
			return conflict("account registration has not completed")
		}
		return s.completeRegistration(ctx, current, result)
	})
}

func (s *Service) RefreshAccountAddTransfer(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	return s.operate(ctx, req.GetFlowId(), req, func(ctx context.Context, current *flow) error {
		if current.snapshot.GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_WAITING_CODE || current.snapshot.GetVerification().GetDeliveryMethod() != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
			return conflict("account adding flow is not waiting for transfer confirmation")
		}
		result := current.protocol.RefreshTransfer(ctx)
		if result.Err != nil {
			return result.Err
		}
		current.snapshot.TransferChallenge = result.Challenge
		return nil
	})
}

func allowedMethod(req *waappv1.BeginAccountAddRequest, method waappv1.VerificationDeliveryMethod) error {
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "QR account transfer is not implemented", false)
	}
	transfer := method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	registration := method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS
	if !transfer && !registration {
		return conflict("verification method is not supported")
	}
	if transfer != (req.GetSource().GetTransfer() != nil) {
		return conflict("verification method does not match adding source")
	}
	return nil
}

func methodStatuses(statuses []wacore.VerificationMethodStatus) []*waappv1.VerificationMethodStatus {
	out := make([]*waappv1.VerificationMethodStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, &waappv1.VerificationMethodStatus{DeliveryMethod: status.Method, Available: status.Available, Cooldown: shared.DurationFromSeconds(status.CooldownSeconds)})
	}
	return out
}

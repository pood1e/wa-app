package onboarding

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestRegistrationRetriesTemporaryLoginFailures(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered", true: "unknown"}[uncertain], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, protocol, acceptor := testService(t)
				response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
				id := response.GetFlow().GetFlowId()
				_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD})
				if uncertain {
					protocol.submitError = errors.New("upstream HTTP 502")
				}
				var attempts []time.Time
				service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
					attempts = append(attempts, time.Now())
					if len(attempts) < 3 {
						return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "temporary connection failure", true)
					}
					return material, nil
				}
				request := &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"}
				for range 2 {
					response, _ = service.SubmitAccountAddCode(t.Context(), request)
					if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
						t.Fatal("temporary login failures required manual recovery")
					}
				}
				if len(attempts) != 3 || attempts[1].Sub(attempts[0]) != time.Second || attempts[2].Sub(attempts[1]) != 2*time.Second {
					t.Fatal("login confirmation did not use bounded backoff")
				}
				if protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || protocol.closes.Load() != 1 || acceptor.calls.Load() != 1 {
					t.Fatal("login retry repeated code delivery, registration, cleanup or acceptance")
				}
			})
		})
	}
}

func TestAutomaticLoginRetryExhaustionRetainsFlow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, protocol, acceptor := testService(t)
		response, _ := service.BeginAccountAdd(t.Context(), beginRequest(true))
		id := response.GetFlow().GetFlowId()
		_, _ = service.RequestAccountAddCode(t.Context(), &waappv1.RequestAccountAddCodeRequest{Context: requestContext("code"), FlowId: id, DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD})
		protocol.submitError = errors.New("upstream HTTP 502")
		checks := 0
		service.validate = func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
			checks++
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "temporary connection failure", true)
		}
		request := &waappv1.SubmitAccountAddCodeRequest{Context: requestContext("submit"), FlowId: id, Code: "012345"}
		for range 2 {
			response, _ = service.SubmitAccountAddCode(t.Context(), request)
			if !proto.Equal(response.GetError(), shared.ToProtoError(protocol.submitError)) || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN {
				t.Fatal("retry exhaustion lost the original error or uncertain state")
			}
		}
		if checks != 3 || protocol.closes.Load() != 0 || acceptor.calls.Load() != 0 || service.lookup(id).protocol != protocol {
			t.Fatal("retry exhaustion replayed requests or discarded unconfirmed keys")
		}
		service.validate = func(_ context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
			return material, nil
		}
		response, _ = service.DeliverAccountAdd(t.Context(), &waappv1.AccountAddRequest{Context: requestContext("manual-check"), FlowId: id})
		if response.GetError() != nil || response.GetFlow().GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || protocol.codes.Load() != 1 || protocol.submits.Load() != 1 || acceptor.calls.Load() != 1 {
			t.Fatal("manual confirmation could not resume after automatic retry exhaustion")
		}
	})
}

func TestLoginValidationStopsOnRejectionOrCancellation(t *testing.T) {
	for _, test := range []struct {
		name     string
		duration time.Duration
		err      error
	}{
		{"rejected", 0, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED, "login rejected", false)},
		{"missing material", 0, nil},
		{"backoff canceled", 500 * time.Millisecond, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "temporary connection failure", true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, _, _ := testService(t)
				ctx := t.Context()
				if test.duration > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.duration)
					defer cancel()
				}
				checks := 0
				service.validate = func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
					checks++
					return nil, test.err
				}
				started := time.Now()
				_, err := service.validateRegistration(ctx, &waappv1.AcceptedAccountMaterial{})
				if err == nil || checks != 1 || time.Since(started) != test.duration {
					t.Fatal("login validation retried a rejection or ignored cancellation")
				}
			})
		})
	}
}

func TestLoginValidationBoundsEachAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, _, _ := testService(t)
		checks := 0
		material := &waappv1.AcceptedAccountMaterial{ServiceLoginId: "synthetic-login"}
		service.validate = func(ctx context.Context, current *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
			checks++
			if current != material {
				t.Error("retry replaced the retained material")
			}
			if checks < 3 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return current, nil
		}
		started := time.Now()
		validated, err := service.validateRegistration(t.Context(), material)
		if err != nil || validated != material || checks != 3 || time.Since(started) != 23*time.Second {
			t.Fatal("login validation did not bound attempts or recover after a timeout")
		}
	})
}

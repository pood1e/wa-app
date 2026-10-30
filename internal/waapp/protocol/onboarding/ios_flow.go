package onboarding

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// IOSFlow adapts host-owned iOS registration and transfer to account adding.
type IOSFlow struct {
	*ios.Flow
	http               *http.Client
	passwordEncryption *security.Client
}

func NewIOSFlow(ctx context.Context, phone *waappv1.PhoneTarget, profile *protocolv1.IOSRegistrationProfile, proxyURL string, dependencies ios.Dependencies) (*IOSFlow, error) {
	transport := &http.Transport{ForceAttemptHTTP2: true}
	if proxyURL != "" {
		proxy, err := wire.ParseOutboundProxyURL(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	flow, err := ios.Begin(ctx, phone, profile, client, dependencies)
	if err != nil {
		client.CloseIdleConnections()
		return nil, iosResultError(nil, err)
	}
	return &IOSFlow{Flow: flow, http: client, passwordEncryption: security.New(nil, client, profile.GetClient(), time.Now, nil)}, nil
}

func (f *IOSFlow) Probe(ctx context.Context) wacore.EngineProbeResult {
	response, err := f.Flow.Probe(ctx)
	result := wacore.EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED}
	if err != nil || response.GetStatus() != "fail" || response.GetReason() != "incorrect" {
		result.Err = iosResultError(response, err)
		return result
	}
	result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE
	result.MethodStatuses = iosMethodStatuses(f.Flow.VerificationMethods())
	for _, method := range result.MethodStatuses {
		if method.Available || method.CooldownSeconds > 0 {
			result.SupportedMethods = append(result.SupportedMethods, method.Method)
		}
	}
	return result
}

func (f *IOSFlow) RequestCode(ctx context.Context, method waappv1.VerificationDeliveryMethod) wacore.EngineCodeResult {
	if err := requireIOSMethod(method); err != nil {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, RawStatus: "fail", Err: err}
	}
	response, err := f.Flow.RequestCode(ctx, method)
	statuses := f.Flow.VerificationMethods()
	result := wacore.EngineCodeResult{RawStatus: response.GetStatus(), RawReason: response.GetReason(),
		Status:             waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
		ExpectedCodeLength: int32(ios.VerificationCodeLength(response, method)), MethodStatuses: iosMethodStatuses(statuses)}
	for _, status := range statuses {
		if status.GetDeliveryMethod() == method {
			result.RetryAfter = status.GetCooldown().AsDuration()
		}
	}
	if err != nil && !f.Flow.OutcomeUnknown() {
		result.RawStatus = "fail"
	}
	if err == nil && response.GetStatus() == "sent" && response.GetMethod() == VerificationMethodCode(method.String()) {
		result.Status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT
	} else {
		result.Err = iosResultError(response, err)
	}
	return result
}

func (f *IOSFlow) SubmitCode(ctx context.Context, method waappv1.VerificationDeliveryMethod, code string) wacore.EngineRegisterResult {
	if err := requireIOSMethod(method); err != nil {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	response, err := f.Flow.SubmitCode(ctx, method, code)
	return f.registerResult(response, err)
}

func (f *IOSFlow) registerResult(response *protocolv1.IOSRegistrationResponse, err error) wacore.EngineRegisterResult {
	result := wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, OutcomeUnknown: f.Flow.OutcomeUnknown()}
	if required, next := f.Flow.PasswordChallenge(); required {
		result.PasswordChallenge = &waappv1.AccountAddPasswordChallenge{NextAttemptAt: shared.ProtoTimestamp(next)}
		if err == nil && response.GetStatus() == "fail" && response.GetReason() == "security_code" {
			result.Status = waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD
			return result
		}
	}
	if err == nil && response.GetStatus() == "ok" {
		result.Status = waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED
		result.ServiceLoginID, result.ServiceAccountID = response.GetLogin(), shared.FirstNonEmpty(response.GetLid(), response.GetLogin())
		result.CompletedAt = time.Now()
	} else if err == nil && (response.GetReason() == "missing" || response.GetReason() == "stale") {
		result.Status = waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE
		result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "verification code must be requested again", true)
	} else {
		result.Err = iosResultError(response, err)
	}
	return result
}

func (f *IOSFlow) SubmitPassword(ctx context.Context, password string) wacore.EngineRegisterResult {
	rejected := func(err error) wacore.EngineRegisterResult {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	if required, next := f.Flow.PasswordChallenge(); !required {
		return rejected(ios.ErrRegistrationState)
	} else if time.Now().Before(next) {
		return rejected(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "two-step verification is cooling down", true))
	}
	key, err := f.passwordEncryption.RegistrationPasswordEncryptionKey(ctx)
	if err != nil {
		return rejected(err)
	}
	sealed, err := security.SealPassword(password, key, time.Now())
	if err != nil {
		return rejected(err)
	}
	response, err := f.Flow.SubmitPassword(ctx, sealed)
	return f.registerResult(response, err)
}

func (f *IOSFlow) RefreshTransfer(context.Context) wacore.EngineAccountTransferChallengeResult {
	return wacore.EngineAccountTransferChallengeResult{Err: requireIOSMethod(waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER)}
}

func (f *IOSFlow) Close() error {
	defer f.http.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return f.Flow.Close(ctx)
}

func requireIOSMethod(method waappv1.VerificationDeliveryMethod) error {
	if method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS && method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD && method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "iOS account adding supports SMS, email and old-device verification", false)
	}
	return nil
}

func iosMethodStatuses(statuses []*waappv1.VerificationMethodStatus) []wacore.VerificationMethodStatus {
	result := make([]wacore.VerificationMethodStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, wacore.VerificationMethodStatus{Method: status.GetDeliveryMethod(), Code: VerificationMethodCode(status.GetDeliveryMethod().String()),
			Available: status.GetAvailable(), CooldownSeconds: int64(status.GetCooldown().AsDuration() / time.Second)})
	}
	return result
}

func iosResultError(response *protocolv1.IOSRegistrationResponse, err error) error {
	if response.GetReason() == "too_recent" || response.GetReason() == "temporarily_unavailable" || errors.Is(err, ios.ErrCodeCooldown) || errors.Is(err, ios.ErrPasswordCooldown) {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "iOS verification is temporarily unavailable or cooling down", true)
	}
	if err != nil {
		message := "iOS registration request or response failed"
		var source interface{ GRPCStatus() *status.Status }
		if errors.As(err, &source) && source.GRPCStatus() != nil {
			rpc := source.GRPCStatus()
			switch rpc.Code() {
			case codes.FailedPrecondition:
				message = "iOS signing context is not ready"
				if rpc.Message() == "device_locked" {
					message = "iOS signing device is locked"
				}
			case codes.ResourceExhausted:
				message = "iOS signing device is busy"
			case codes.DeadlineExceeded:
				message = "iOS signing operation timed out"
			default:
				message = "iOS signing service is unavailable"
			}
		}
		var upstream *ios.HTTPStatusError
		if errors.As(err, &upstream) {
			message = fmt.Sprintf("iOS registration server returned HTTP %d", upstream.StatusCode)
		}
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, message, true)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "iOS verification was rejected", false)
}

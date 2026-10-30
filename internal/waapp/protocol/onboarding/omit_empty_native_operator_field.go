package onboarding

import (
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func OmitEmptyNativeOperatorField(key string, value string) bool {
	if strings.TrimSpace(value) != "" {
		return false
	}
	switch key {
	case "mcc", "mnc", "sim_mcc", "sim_mnc":
		return true
	default:
		return false
	}
}

func SanitizeResponse(data map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range data {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "auth") || strings.Contains(lower, "code") || strings.Contains(lower, "sig") || strings.Contains(lower, "routing") {
			out[key] = "<redacted>"
			continue
		}
		if lower == "response_text" {
			out[key] = pstate.SafeResponseSnippet(pstate.JsonString(value))
			continue
		}
		out[key] = value
	}
	return out
}

func VerificationCodeResult(status waappv1.VerificationRequestStatus, data map[string]any, method waappv1.VerificationDeliveryMethod, now time.Time, retryAfter time.Duration) wacore.EngineCodeResult {
	return wacore.EngineCodeResult{
		Status:             status,
		ExpectedCodeLength: int32(pstate.JsonNumber(data["length"])),
		ExpiresAt:          now.Add(10 * time.Minute),
		RetryAfter:         retryAfter,
		MethodStatuses:     VerificationCodeMethodStatuses(data, method),
		RawStatus:          pstate.ResponseStatus(data),
		RawReason:          ResponseReason(data),
	}
}

func VerificationCodeRejectedResult(data map[string]any, method waappv1.VerificationDeliveryMethod, now time.Time, retryAfter time.Duration, fallback string) wacore.EngineCodeResult {
	return wacore.EngineCodeResult{
		Status:             waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
		ExpectedCodeLength: int32(pstate.JsonNumber(data["length"])),
		ExpiresAt:          now.Add(10 * time.Minute),
		RetryAfter:         retryAfter,
		MethodStatuses:     VerificationCodeMethodStatuses(data, method),
		RawStatus:          pstate.ResponseStatus(data),
		RawReason:          ResponseReason(data),
		Err:                WaProtocolError(data, fallback),
	}
}

func VerificationCodeRateLimited(data map[string]any) bool {
	switch pstate.ResponseStatus(data) {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	}
	switch ResponseReason(data) {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

func VerificationCodeRetryAfter(data map[string]any, method waappv1.VerificationDeliveryMethod) time.Duration {
	seconds := max(VerificationMethodWaitStatus(data, RegistrationMethodName(method, "sms"), false).Seconds, FirstJSONWaitStatus(data["retry_after"]).Seconds)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func ClassifyHTTPError(data map[string]any, err error) error {
	status := pstate.ResponseStatus(data)
	switch status {
	case "no_routes":
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "no_routes: verification route is unavailable", false)
	case "too_recent":
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "verification request is too recent", true)
	case "blocked", "rejected":
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, status+": request was rejected", false)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, pstate.UpstreamFailureMessage(data, err), true)
}

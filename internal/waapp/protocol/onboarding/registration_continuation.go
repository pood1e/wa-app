package onboarding

import (
	"slices"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func registrationContinuation(data map[string]any, current waappv1.VerificationDeliveryMethod) (*waappv1.VerificationMethodStatus, time.Duration, error) {
	if state.ResponseStatus(data) != "fail" {
		return nil, 0, nil
	}
	var code string
	switch ResponseReason(data) {
	case "sms_required":
		code = "sms"
	case "next_method_required":
		methods, _ := data["fallback_methods"].([]any)
		// The native client selects the immediate successor, retaining server order.
		next := slices.IndexFunc(methods, func(value any) bool {
			return state.JsonString(value) == RegistrationMethodName(current, "")
		}) + 1
		if next >= len(methods) {
			return nil, 0, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "registration did not provide its next verification method", false)
		}
		code = state.JsonString(methods[next])
	default:
		return nil, 0, nil
	}
	method := VerificationMethodFromName(code)
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS:
	default:
		return nil, 0, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "required verification method is unsupported", false)
	}
	wait := VerificationMethodWaitStatus(data, VerificationMethodCode(code), false)
	if wait.Exhausted {
		return nil, 0, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "required verification method is unavailable", false)
	}
	seconds := min(max(wait.Seconds, FirstJSONWaitStatus(data["retry_after"]).Seconds), int64((1<<63-1)/time.Second))
	retry := time.Duration(seconds) * time.Second
	return &waappv1.VerificationMethodStatus{DeliveryMethod: method, Available: retry <= 0, Cooldown: shared.DurationToProto(retry)}, retry, nil
}

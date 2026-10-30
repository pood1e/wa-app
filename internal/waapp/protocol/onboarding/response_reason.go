package onboarding

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ResponseReason(data map[string]any) string {
	if value, ok := data["reason"].(string); ok {
		return strings.ToLower(value)
	}
	if value, ok := data["failure_reason"].(string); ok {
		return strings.ToLower(value)
	}
	return ""
}

func ExistReachableStatus(status string) bool {
	switch status {
	case "ok", "sent", "valid", "exists", "registered":
		return true
	default:
		return false
	}
}

func ExistRegisteredStatus(status string) bool {
	switch status {
	case "exists", "registered":
		return true
	default:
		return false
	}
}

func ExistProtocolRejected(status string, reason string) bool {
	if status == "" && reason == "" {
		return false
	}
	switch reason {
	case "missing_param", "bad_param", "bad_token", "old_version", "invalid_skey":
		return true
	default:
		return false
	}
}

func ExistInvalidNumberReason(reason string) bool {
	switch reason {
	case "format_wrong", "length_short", "length_long":
		return true
	default:
		return false
	}
}

func ExistRateLimitedReason(reason string) bool {
	switch reason {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

// existNotRegisteredReason reports reasons that the same-device check
// (KotlinRegistrationBridge.parseSameDeviceCheckResponse) treats as "no
// existing account on this device, proceed to normal registration".
// "incorrect" is the canonical such verdict: it is an expected /v2/exist
// response, not a request error, so the probe resolves to a definitive
// not-registered/registrable result instead of the reachable catch-all.
func ExistNotRegisteredReason(reason string) bool {
	return reason == "incorrect"
}

func ExistRegisteredSignal(status string, reason string, data map[string]any) bool {
	if ExistRegisteredReason(reason) {
		return true
	}
	if ExistRegisteredStatus(status) {
		return true
	}
	return shared.FirstNonEmpty(pstate.JsonString(data["new_jid"]), pstate.JsonString(data["jid"]), pstate.JsonString(data["registration_jid"])) != ""
}

func ExistRegisteredReason(reason string) bool {
	switch reason {
	case "security_code", "second_code", "device_confirm_or_second_code", "consent_parent_linking_already_registered":
		return true
	default:
		return false
	}
}

// existConsentReason reports reasons where the number is registrable but the
// same-device check (KotlinRegistrationBridge.parseSameDeviceCheckResponse)
// requires the consent (age/parental) flow before a code can be requested.
func ExistConsentReason(reason string) bool {
	switch reason {
	case "consent", "consent_minor", "app_store_age":
		return true
	default:
		return false
	}
}

// existConsentBlockedReason reports consent verdicts that hard-block
// registration: underage, impossible age, parental block, linking ineligible.
func ExistConsentBlockedReason(reason string) bool {
	switch reason {
	case "consent_underage_block", "consent_impossible_age", "consent_parent_block", "consent_parent_linking_ineligible":
		return true
	default:
		return false
	}
}

// existChallengeReason reports reasons that require the challenge flow
// (email/captcha checkpoint) before registration can proceed.
func ExistChallengeReason(reason string) bool {
	switch reason {
	case "challenge", "challenge_email_start":
		return true
	default:
		return false
	}
}

type ExistFlowClass struct {
	ProtocolRejected  bool
	Registered        bool
	NotRegistered     bool
	Blocked           bool
	InvalidNumber     bool
	RateLimited       bool
	ConsentRequired   bool
	ChallengeRequired bool
}

func ExistAccountFlow(c ExistFlowClass) string {
	switch {
	case c.ProtocolRejected:
		return AccountProbeFlowProbeFailed
	case c.Blocked:
		return AccountProbeFlowBlocked
	case c.InvalidNumber:
		return AccountProbeFlowInvalidNumber
	case c.RateLimited:
		return AccountProbeFlowRateLimited
	case c.ConsentRequired:
		return AccountProbeFlowConsentRequired
	case c.ChallengeRequired:
		return AccountProbeFlowChallengeRequired
	case c.Registered:
		return AccountProbeFlowRegistered
	case c.NotRegistered:
		return AccountProbeFlowNotRegistered
	default:
		return AccountProbeFlowUnknown
	}
}

func ExistProtocolError(data map[string]any) error {
	return WaProtocolError(data, "WA exist probe rejected")
}

func WaProtocolError(data map[string]any, fallback string) error {
	reason := ResponseReason(data)
	param := pstate.JsonString(data["param"])
	message := fallback
	if reason != "" {
		message += ": reason=" + reason
	}
	if param != "" {
		message += " param=" + param
	}
	code := waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED
	retryable := false
	switch {
	case VerificationCodeRateLimited(data):
		code = waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED
		retryable = true
	case reason == "no_routes":
		code = waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE
	}
	return shared.NewError(code, message, retryable)
}

func MethodsFromStatuses(statuses []wacore.VerificationMethodStatus) []waappv1.VerificationDeliveryMethod {
	seen := map[waappv1.VerificationDeliveryMethod]struct{}{}
	out := make([]waappv1.VerificationDeliveryMethod, 0, len(statuses))
	for _, status := range statuses {
		if status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			continue
		}
		if _, ok := seen[status.Method]; ok {
			continue
		}
		seen[status.Method] = struct{}{}
		out = append(out, status.Method)
	}
	return out
}

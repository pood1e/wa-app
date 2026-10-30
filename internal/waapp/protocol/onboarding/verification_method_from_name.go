package onboarding

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func VerificationMethodFromName(name string) waappv1.VerificationDeliveryMethod {
	switch VerificationMethodCode(name) {
	case "sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH
	case "wa_old":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	case "email_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP
	case "send_sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS
	case "passkey":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY
	case "silent_auth":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH
	case "silent_auth_ts_43":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43
	case "autoconf":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_AUTOCONF
	case "deeplink_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DEEPLINK_OTP
	case "recaptcha":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA
	case "oauth_email":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL
	case "discoverable_credential":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL
	case "acc_tr":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	case "standalone":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

type VerificationWaitStatus struct {
	Seconds   int64
	Exhausted bool
	Present   bool
}

var ApkDefaultRegistrationMethodOrder = []string{"flash", "sms", "voice", "wa_old", "acc_tr", "send_sms", "email_otp"}

func VerificationMethodStatuses(data map[string]any, _ []waappv1.VerificationDeliveryMethod) []wacore.VerificationMethodStatus {
	out := []wacore.VerificationMethodStatus{}
	for _, code := range ApkVisibleFallbackMethodCodes(data) {
		out = UpsertVerificationMethodStatus(out, code, VerificationMethodWaitStatus(data, code, false))
	}
	return out
}

func VerificationCodeMethodStatuses(data map[string]any, _ waappv1.VerificationDeliveryMethod) []wacore.VerificationMethodStatus {
	return VerificationMethodStatuses(data, nil)
}

func UpsertVerificationMethodStatus(statuses []wacore.VerificationMethodStatus, code string, wait VerificationWaitStatus) []wacore.VerificationMethodStatus {
	method := VerificationMethodFromName(code)
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		return statuses
	}
	for i := range statuses {
		if statuses[i].Code == code || statuses[i].Method == method {
			statuses[i] = wacore.VerificationMethodStatus{Method: method, Code: code, Available: wait.Seconds <= 0 && !wait.Exhausted, CooldownSeconds: wait.Seconds}
			return statuses
		}
	}
	return append(statuses, wacore.VerificationMethodStatus{Method: method, Code: code, Available: wait.Seconds <= 0 && !wait.Exhausted, CooldownSeconds: wait.Seconds})
}

func VerificationMethodCode(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "verification_delivery_method_")
	normalized = strings.TrimPrefix(normalized, "registration_login_method_")
	switch normalized {
	case "sms":
		return "sms"
	case "send_sms", "send-sms", "send_sms_to_wa", "send-sms-to-wa":
		return "send_sms"
	case "voice", "call", "phone_call":
		return "voice"
	case "flash":
		return "flash"
	case "wa_old", "wa-old", "old_wa":
		return "wa_old"
	case "email", "email_otp", "email-otp":
		return "email_otp"
	case "passkey":
		return "passkey"
	case "silent_auth", "silent-auth":
		return "silent_auth"
	case "silent_auth_ts_43", "silent-auth-ts-43", "silent_auth_ts43":
		return "silent_auth_ts_43"
	case "autoconf", "auto_conf", "auto-confirm":
		return "autoconf"
	case "deeplink_otp", "deeplink-otp", "deep_link_otp":
		return "deeplink_otp"
	case "recaptcha":
		return "recaptcha"
	case "oauth_email", "oauth-email":
		return "oauth_email"
	case "discoverable_credential", "discoverable-credential":
		return "discoverable_credential"
	case "acc_tr", "account_transfer", "account-transfer":
		return "acc_tr"
	case "standalone", "acverify", "app":
		return "standalone"
	default:
		return ""
	}
}

func FallbackVerificationMethodCodes(data map[string]any) []string {
	return VerificationMethodCodesFromValue(data["fallback_methods"])
}

func PrefRegistrationMethodOrderCodes(data map[string]any) []string {
	if codes := VerificationMethodCodesFromValue(data["pref_reg_methods_order"]); len(codes) > 0 {
		return codes
	}
	return append([]string(nil), ApkDefaultRegistrationMethodOrder...)
}

func VerificationMethodCodesFromValue(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range pstate.StringList(value) {
		code := VerificationMethodCode(raw)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func ApkVisibleFallbackMethodCodes(data map[string]any) []string {
	fallback := FallbackVerificationMethodCodeSet(data)
	if len(fallback) == 0 {
		return nil
	}
	out := []string{}
	for _, code := range PrefRegistrationMethodOrderCodes(data) {
		if !fallback[code] {
			continue
		}
		wait := VerificationMethodWaitStatus(data, code, false)
		if !wait.Present || wait.Exhausted {
			continue
		}
		if !VerificationMethodEligibleForAPKUI(data, code) {
			continue
		}
		out = append(out, code)
	}
	return out
}

func FallbackVerificationMethodCodeSet(data map[string]any) map[string]bool {
	codes := FallbackVerificationMethodCodes(data)
	if len(codes) == 0 {
		return nil
	}
	out := make(map[string]bool, len(codes))
	for _, code := range codes {
		out[code] = true
	}
	return out
}

func WaOldFallbackEligible(data map[string]any) bool {
	for _, code := range FallbackVerificationMethodCodes(data) {
		if code == "wa_old" {
			return VerificationMethodEligibleForAPKUI(data, code)
		}
	}
	return false
}

func AccountTransferFallbackEligible(data map[string]any) bool {
	for _, code := range FallbackVerificationMethodCodes(data) {
		if code == "acc_tr" {
			return VerificationMethodEligibleForAPKUI(data, code)
		}
	}
	return false
}

func VerificationMethodEligibleForAPKUI(data map[string]any, code string) bool {
	switch code {
	case "sms", "voice", "flash":
		return true
	case "wa_old":
		eligibility, ok := FirstPresentJSONInt64(data["pref_wa_old_eligibility"], data["wa_old_eligible"])
		if !ok {
			return false
		}
		return eligibility != 0 && eligibility != 4
	case "acc_tr":
		if VerificationExplicitlyEligible(data, "pref_acc_tr_eligibility", "acc_tr_eligible", "account_transfer_eligible") {
			return true
		}
		return WaOldFallbackEligible(data)
	case "send_sms":
		return VerificationExplicitlyEligible(data, "pref_send_sms_eligibility", "send_sms_eligible", "can_send_sms_to_wa") && !VerificationExplicitlyExhausted(data, "send_sms_attempts_exhausted", "pref_send_sms_attempts_exhausted")
	case "email_otp":
		eligibility, ok := FirstPresentJSONInt64(data["pref_email_otp_eligibility"], data["email_otp_eligible"])
		return ok && eligibility == 1
	case "silent_auth", "silent_auth_ts_43":
		return VerificationExplicitlyEligible(data, "pref_silent_auth_eligibility", "silent_auth_eligible", "silent_auth_available")
	default:
		return false
	}
}

func VerificationExplicitlyEligible(data map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key].(bool); ok {
			return value
		}
		if value, ok := FirstPresentJSONInt64(data[key]); ok {
			return value == 1
		}
	}
	return false
}

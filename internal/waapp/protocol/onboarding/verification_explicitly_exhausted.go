package onboarding

import (
	"strings"
	"time"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func VerificationExplicitlyExhausted(data map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key].(bool); ok && value {
			return true
		}
		if value, ok := FirstPresentJSONInt64(data[key]); ok && value != 0 {
			return true
		}
	}
	return false
}

func VerificationMethodWaitStatus(data map[string]any, code string, includeRetryAfter bool) VerificationWaitStatus {
	wait := FirstJSONWaitStatus(VerificationMethodWaitValues(data, code)...)
	if wait.Present || !includeRetryAfter {
		return wait
	}
	return FirstJSONWaitStatus(data["retry_after"])
}

func VerificationMethodWaitValues(data map[string]any, code string) []any {
	switch code {
	case "sms":
		return []any{data["sms_wait"], data["sms_wait_time"], data["sms_retry_time"], data["pref_sms_wait_time"], data["EXTRA_SMS_RETRY_TIME"]}
	case "send_sms":
		return []any{data["send_sms_wait"], data["send_sms_retry_time"], data["pref_send_sms_wait_time"], data["EXTRA_SEND_SMS_RETRY_TIME"]}
	case "voice":
		return []any{data["voice_wait"], data["voice_wait_time"], data["voice_retry_time"], data["pref_voice_wait_time"], data["EXTRA_VOICE_RETRY_TIME"]}
	case "flash":
		return []any{data["flash_wait"], data["flash_wait_time"], data["flash_retry_time"], data["pref_flash_wait_time"], data["EXTRA_FLASH_RETRY_TIME"]}
	case "wa_old":
		return []any{data["wa_old_wait"], data["wa_old_retry_time"], data["pref_wa_old_wait_time"], data["EXTRA_WA_OLD_RETRY_TIME"]}
	case "acc_tr":
		return []any{data["acc_tr_wait"], data["account_transfer_wait"], data["pref_acc_tr_wait_time"], data["EXTRA_ACC_TR_RETRY_TIME"]}
	case "email_otp":
		return []any{data["email_otp_wait"], data["email_otp_retry_time"], data["pref_email_otp_wait_time"], data["EXTRA_EMAIL_OTP_RETRY_TIME"]}
	case "silent_auth":
		return []any{data["silent_auth_wait"], data["pref_silent_auth_wait_time"]}
	default:
		return nil
	}
}

func VerificationSMSCooldownSeconds(data map[string]any) int64 {
	return VerificationMethodWaitStatus(data, "sms", true).Seconds
}

func VerificationSMSWaitExhausted(data map[string]any) bool {
	return VerificationMethodWaitStatus(data, "sms", true).Exhausted
}

func SmsProbeAvailableByCooldownOnly(smsWait int64, smsWaitExhausted bool, blocked bool, protocolRejected bool, invalidNumber bool, rateLimited bool) bool {
	return smsWait <= 0 && !smsWaitExhausted && !blocked && !protocolRejected && !invalidNumber && !rateLimited
}

func FirstPresentJSONInt64(values ...any) (int64, bool) {
	for _, value := range values {
		if JsonValuePresent(value) {
			return pstate.JsonInt64(value), true
		}
	}
	return 0, false
}

func FirstJSONWaitStatus(values ...any) VerificationWaitStatus {
	for _, value := range values {
		if !JsonValuePresent(value) {
			continue
		}
		raw := pstate.JsonInt64(value)
		if raw < 0 {
			return VerificationWaitStatus{Exhausted: true, Present: true}
		}
		return VerificationWaitStatus{Seconds: NormalizeWaitSeconds(raw), Present: true}
	}
	return VerificationWaitStatus{}
}

func NormalizeWaitSeconds(value int64) int64 {
	if value <= 0 {
		return 0
	}
	now := time.Now()
	nowMS := now.UnixMilli()
	if value >= 1_000_000_000_000 {
		if value <= nowMS {
			return 0
		}
		return (value - nowMS + 999) / 1000
	}
	nowSeconds := now.Unix()
	if value >= 1_000_000_000 {
		if value <= nowSeconds {
			return 0
		}
		return value - nowSeconds
	}
	return value
}

func JsonValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

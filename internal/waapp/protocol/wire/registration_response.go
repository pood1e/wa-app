package wire

import (
	"encoding/json"
	"log"
	"math"
	"net/url"
	"slices"
	"strconv"
)

var (
	registrationResponseStatuses = []string{
		"sent", "ok", "fail", "registered", "exists", "valid", "rejected", "blocked",
		"too_recent", "too_many", "temporarily_unavailable",
	}
	registrationResponseReasons = []string{
		"missing", "stale", "mismatch", "incorrect", "missing_param", "bad_param", "bad_token",
		"invalid_skey", "old_version", "no_routes", "too_recent", "too_many", "too_many_guesses",
		"guessed_too_fast", "temporarily_unavailable", "blocked", "not_allowed", "next_method",
		"provider_timeout", "provider_unroutable", "format_wrong", "length_short", "length_long",
		"security_code", "second_code", "device_confirm_or_second_code", "challenge", "consent",
	}
	registrationResponseParameters = []string{
		"cc", "in", "lg", "lc", "fdid", "expid", "id", "backup_token", "token", "authkey",
		"e_ident", "e_keytype", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig", "type", "method",
		"code", "auth_response", "context", "login", "advertising_id", "access_session_id", "gpia", "H",
	}
)

// LogRegistrationResponse records allowlisted diagnostics from a bounded JSON response.
func LogRegistrationResponse(endpoint *url.URL, statusCode int, body []byte, enc string) {
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	logRegistrationResponse(endpoint, statusCode, data, enc)
}

func logRegistrationResponse(endpoint *url.URL, statusCode int, data map[string]any, enc string) {
	if kind := NativeRegistrationEndpointKind(endpoint); kind != "" {
		hash := "absent"
		if enc != "" {
			hash = EncHash(enc)
		}
		log.Printf("wa_registration_response_shape kind=%s enc_sha256=%s status_code=%d status=%s reason=%s param=%s method=%s error_code=%s retry_after=%s length=%s auth_response_len=%d context_len=%d",
			kind, hash, statusCode,
			registrationResponseLabel(data, "status", registrationResponseStatuses...),
			registrationResponseLabel(data, "reason", registrationResponseReasons...),
			registrationResponseLabel(data, "param", registrationResponseParameters...),
			registrationResponseLabel(data, "method", "sms", "voice", "flash", "wa_old", "email_otp", "send_sms", "acc_tr"),
			registrationErrorCode(data), registrationResponseInteger(data, "retry_after"), registrationResponseInteger(data, "length"),
			registrationResponseLength(data, "auth_response"), registrationResponseLength(data, "context"))
	}
}

func registrationErrorCode(data map[string]any) string {
	return registrationResponseInteger(data, "error_code")
}

func registrationResponseLabel(data map[string]any, field string, allowed ...string) string {
	value, exists := data[field]
	if !exists {
		return "absent"
	}
	if label, ok := value.(string); ok && slices.Contains(allowed, label) {
		return label
	}
	return "unrecognized"
}

func registrationResponseLength(data map[string]any, field string) int {
	value, _ := data[field].(string)
	return len(value)
}

func registrationResponseInteger(data map[string]any, field string) string {
	value, exists := data[field]
	if !exists {
		return "absent"
	}
	switch code := value.(type) {
	case float64:
		if math.Trunc(code) == code && code >= math.MinInt32 && code <= math.MaxInt32 {
			return strconv.FormatInt(int64(code), 10)
		}
	case string:
		if parsed, err := strconv.ParseInt(code, 10, 32); err == nil {
			return strconv.FormatInt(parsed, 10)
		}
	}
	return "invalid"
}

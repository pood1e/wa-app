package onboarding

import (
	"encoding/base64"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func SetNonEmptyFormParam(params *OrderedParams, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	params.Set(key, value, false)
}

// ApplyNativeRegisterRequestMapParams builds verification observations.
func ApplyNativeRegisterRequestMapParams(params *OrderedParams, fields map[string]string, method string) {
	AddOptionalRawParam(params, "mistyped", fields["mistyped"])
	AddRawParam(params, "client_metrics", NativeRegisterClientMetrics(method))
	AddRawParam(params, "entered", NativeCodeEntryMethod(method))
	AddOptionalRawParam(params, "mcc", fields["mcc"])
	AddOptionalRawParam(params, "mnc", fields["mnc"])
	AddOptionalRawParam(params, "sim_mcc", fields["sim_mcc"])
	AddOptionalRawParam(params, "sim_mnc", fields["sim_mnc"])
	AddOptionalRawParam(params, "network_operator_name", fields["network_operator_name"])
	AddOptionalRawParam(params, "sim_operator_name", fields["sim_operator_name"])
	AddOptionalRawParam(params, "network_radio_type", fields["network_radio_type"])
	AddOptionalRawParam(params, "simnum", fields["simnum"])
	AddOptionalRawParam(params, "hasinrc", fields["hasinrc"])
	AddOptionalRawParam(params, "pid", fields["pid"])
	AddOptionalRawParam(params, "rc", fields["rc"])
	ApplyNativeCodeRequestSIMSignalParams(params, fields)
}

// ApplyNativeRegisterRequestTailParams retains verification PAA and credential order.
func ApplyNativeRegisterRequestTailParams(params *OrderedParams, fields map[string]string) {
	AddOptionalRawParam(params, "recaptcha", fields["recaptcha"])
	AddOptionalRawParam(params, "fid", fields["fid"])
	AddOptionalRawParam(params, "preloads_app_manager_id", fields["preloads_app_manager_id"])
	AddOptionalRawParam(params, "preloads_attribution", fields["preloads_attribution"])
	AddOptionalRawParam(params, "tos_version", fields["tos_version"])
	AddOptionalRawParam(params, "cred_token", fields["cred_token"])
	AddOptionalRawParam(params, "create_paa", fields["create_paa"])
	AddOptionalRawParam(params, "entrypoint", fields["entrypoint"])
	AddOptionalRawParam(params, "suma", fields["suma"])
}

func NativeRegistrationMethodUsesAuthContext(methodName string) bool {
	return methodName != "acc_tr"
}

func ApplyNativeE2EParams(params *OrderedParams, state pstate.NativeState) error {
	if key := state.Signal.PQLastResortPreKey; key != nil {
		identity, err := state.Signal.Identity.PublicBytes()
		if err != nil {
			return err
		}
		if err := wire.ValidateSignalPQPreKey(key, identity); err != nil {
			return err
		}
	}
	params.Set("authkey", state.AuthKey, false)
	params.Set("e_ident", state.KeyBundle.IdentityPublic, false)
	params.Set("e_keytype", state.KeyBundle.KeyType, false)
	params.Set("e_regid", state.KeyBundle.RegID, false)
	params.Set("e_skey_id", state.KeyBundle.SignedKeyID, false)
	params.Set("e_skey_val", state.KeyBundle.SignedKeyValue, false)
	params.Set("e_skey_sig", state.KeyBundle.SignedKeySig, false)
	if key := state.Signal.PQLastResortPreKey; key != nil {
		id := uint32(key.GetId())
		params.Set("e_pq_last_resort_id", base64.RawURLEncoding.EncodeToString([]byte{byte(id >> 16), byte(id >> 8), byte(id)}), false)
		params.Set("e_pq_last_resort_val", base64.RawURLEncoding.EncodeToString(key.GetPublic()), false)
		params.Set("e_pq_last_resort_sig", base64.RawURLEncoding.EncodeToString(key.GetSignature()), false)
	}
	return nil
}

// ApplyNativeCodeRequestMapParams builds RequestCodeRepository additional parameters.
func ApplyNativeCodeRequestMapParams(params *OrderedParams, fields map[string]string, method string, attempts int, reason string) {
	AddOptionalRawParam(params, "mistyped", fields["mistyped"])
	AddRawParam(params, "reason", reason)
	AddOptionalRawParam(params, "hasav", fields["hasav"])
	AddRawParam(params, "client_metrics", NativeCodeClientMetrics(attempts))
	AddOptionalRawParam(params, "mcc", fields["mcc"])
	AddOptionalRawParam(params, "mnc", fields["mnc"])
	AddOptionalRawParam(params, "sim_mcc", fields["sim_mcc"])
	AddOptionalRawParam(params, "sim_mnc", fields["sim_mnc"])
	AddRawParam(params, "education_screen_displayed", "false")
	AddRawParam(params, "prefer_sms_over_flash", NativePreferSMSOverFlash(method, fields))
	AddOptionalRawParam(params, "network_radio_type", fields["network_radio_type"])
	AddOptionalRawParam(params, "simnum", fields["simnum"])
	AddOptionalRawParam(params, "hasinrc", fields["hasinrc"])
	AddOptionalRawParam(params, "pid", fields["pid"])
	AddOptionalRawParam(params, "rc", fields["rc"])
	ApplyNativeCodeRequestSIMSignalParams(params, fields)
	ApplyNativeCodeRequestStoredParams(params, fields)
}

func ApplyNativeCodeRequestSIMSignalParams(params *OrderedParams, fields map[string]string) {
	AddOptionalRawParam(params, "sim_type", fields["sim_type"])
	AddOptionalRawParam(params, "airplane_mode_type", fields["airplane_mode_type"])
	AddOptionalRawParam(params, "cellular_strength", fields["cellular_strength"])
	AddOptionalRawParam(params, "roaming_type", fields["roaming_type"])
}

func ApplyNativeCodeRequestStoredParams(params *OrderedParams, fields map[string]string) {
	AddOptionalRawParam(params, "push_code", fields["push_code"])
	AddOptionalRawParam(params, "new_acc_uuid", fields["new_acc_uuid"])
}

func AddOptionalRawParam(params *OrderedParams, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	AddRawParam(params, key, value)
}

func AddRawParam(params *OrderedParams, key string, value string) {
	params.Set(key, pstate.PctBytes([]byte(value)), true)
}

func RegistrationMethodName(method waappv1.VerificationDeliveryMethod, fallback string) string {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return "sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE:
		return "voice"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
		return "wa_old"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH:
		return "flash"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
		return "email_otp"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS:
		return "send_sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY:
		return "passkey"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH:
		return "silent_auth"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43:
		return "silent_auth_ts_43"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_AUTOCONF:
		return "autoconf"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DEEPLINK_OTP:
		return "deeplink_otp"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA:
		return "recaptcha"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL:
		return "oauth_email"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL:
		return "discoverable_credential"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER:
		return "acc_tr"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP:
		return "standalone"
	default:
		return fallback
	}
}

func CodeDeviceMap(method string, state pstate.NativeState) map[string]string {
	fields := NativeDeviceMapFields(state)
	out := map[string]string{
		"reason":                     NativeCodeRequestReason(state),
		"client_metrics":             NativeCodeClientMetrics(NativeCodeRequestAttempts(state)),
		"education_screen_displayed": "false",
		"prefer_sms_over_flash":      NativePreferSMSOverFlash(method, fields),
		"network_radio_type":         fields["network_radio_type"],
		"simnum":                     fields["simnum"],
		"hasinrc":                    fields["hasinrc"],
		"pid":                        fields["pid"],
		"rc":                         fields["rc"],
		"mcc":                        fields["mcc"],
		"mnc":                        fields["mnc"],
		"sim_mcc":                    fields["sim_mcc"],
		"sim_mnc":                    fields["sim_mnc"],
	}
	AddNonEmptyNativeCodeField(out, fields, "mistyped")
	AddNonEmptyNativeCodeField(out, fields, "hasav")
	for _, key := range []string{
		"sim_type", "airplane_mode_type", "cellular_strength", "roaming_type",
		"push_code", "new_acc_uuid", "old_phone_number", "device_ram",
		"db", "recaptcha", "fid", "preloads_app_manager_id", "preloads_attribution",
		"tos_version", "entrypoint", "cred_token",
	} {
		AddNonEmptyNativeCodeField(out, fields, key)
	}
	return out
}

func NativePreferSMSOverFlash(method string, fields map[string]string) string {
	_ = method
	return shared.FirstNonEmpty(fields["prefer_sms_over_flash"], "false")
}

func AddNonEmptyNativeCodeField(out map[string]string, fields map[string]string, key string) {
	if value := strings.TrimSpace(fields[key]); value != "" {
		out[key] = value
	}
}

func RegisterDeviceMap(method string, state pstate.NativeState) map[string]string {
	fields := NativeDeviceMapFields(state)
	return map[string]string{
		"mistyped":              "7",
		"client_metrics":        NativeRegisterClientMetrics(method),
		"entered":               NativeCodeEntryMethod(method),
		"mcc":                   fields["mcc"],
		"mnc":                   fields["mnc"],
		"sim_mcc":               fields["sim_mcc"],
		"sim_mnc":               fields["sim_mnc"],
		"network_operator_name": fields["network_operator_name"],
		"sim_operator_name":     fields["sim_operator_name"],
		"network_radio_type":    fields["network_radio_type"],
		"simnum":                fields["simnum"],
		"hasinrc":               fields["hasinrc"],
		"pid":                   fields["pid"],
		"rc":                    fields["rc"],
	}
}

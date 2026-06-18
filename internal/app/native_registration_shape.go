package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func logNativeRegistrationOrderedShape(kind string, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, params orderedParams) {
	if len(params) == 0 {
		return
	}
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = stableID(phone.GetE164Number())
	}
	log.Printf(
		"wa_registration_request_shape kind=%s phone_hash=%s method=%s field_count=%d fields=%s",
		probeLogValue(kind),
		phoneHash,
		probeLogValue(registrationMethodName(method, "sms")),
		len(params),
		registrationShapeFields(params),
	)
	logNativeRegistrationValueHashes(kind, phoneHash, method, params)
	logNativeRegistrationWamsysGAShape(kind, phoneHash, method, params)
}

func logNativeRegistrationMapShape(kind string, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, params map[string]string, rawKeys map[string]struct{}) {
	if len(params) == 0 {
		return
	}
	ordered := make(orderedParams, 0, len(params))
	for _, key := range stableParamOrder(params) {
		_, raw := rawKeys[key]
		ordered = append(ordered, orderedParam{key: key, val: params[key], raw: raw})
	}
	logNativeRegistrationOrderedShape(kind, phone, method, ordered)
}

func registrationShapeFields(params orderedParams) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		mode := "form"
		if param.raw {
			mode = "raw"
		}
		parts = append(parts, param.key+":"+strconv.Itoa(registrationShapeValueLength(param.val, param.raw))+":"+mode)
	}
	return strings.Join(parts, ",")
}

func registrationShapeValueLength(value string, raw bool) int {
	if !raw {
		return len([]byte(value))
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return len([]byte(value))
	}
	return len([]byte(decoded))
}

func logNativeRegistrationValueHashes(kind string, phoneHash string, method waappv1.VerificationDeliveryMethod, params orderedParams) {
	values := registrationValueHashes(params)
	if values == "" {
		return
	}
	log.Printf(
		"wa_registration_value_hashes kind=%s phone_hash=%s method=%s values=%s",
		probeLogValue(kind),
		phoneHash,
		probeLogValue(registrationMethodName(method, "sms")),
		values,
	)
}

func registrationValueHashes(params orderedParams) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		if !shouldLogRegistrationValueHash(param.key) {
			continue
		}
		value := registrationHashValue(param.val, param.raw)
		parts = append(parts, param.key+":"+strconv.Itoa(len([]byte(value)))+":"+stableID(value))
	}
	return strings.Join(parts, ",")
}

func registrationHashValue(value string, raw bool) string {
	if !raw {
		return value
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func shouldLogRegistrationValueHash(key string) bool {
	switch key {
	case "fdid", "expid", "access_session_id", "id", "backup_token", "token",
		"authkey", "e_ident", "e_keytype", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig",
		"mistyped", "reason", "hasav", "client_metrics", "mcc", "mnc", "sim_mcc", "sim_mnc",
		"education_screen_displayed", "prefer_sms_over_flash", "network_radio_type", "simnum",
		"hasinrc", "pid", "rc", "sim_type", "airplane_mode_type", "cellular_strength",
		"roaming_type", "push_code", "new_acc_uuid", "old_phone_number", "device_ram", "db", "recaptcha",
		"fid", "preloads_app_manager_id", "preloads_attribution", "tos_version", "entrypoint",
		"cred_token", "feo2_query_status",
		"ab_hash", "gpia", "_ge", "_gi", "_gg", "_gp", "_ga", "_gs", "aid":
		return true
	default:
		return false
	}
}

func logNativeRegistrationWamsysGAShape(kind string, phoneHash string, method waappv1.VerificationDeliveryMethod, params orderedParams) {
	for _, param := range params {
		if param.key != "_ga" {
			continue
		}
		value := registrationHashValue(param.val, param.raw)
		var shape struct {
			BootID          string `json:"bi"`
			SourcePathAge   int64  `json:"ap"`
			DataPathAge     int64  `json:"ai"`
			ExternalPathAge int64  `json:"ae"`
			MultiProcess    bool   `json:"mp"`
			MultiUser       bool   `json:"mu"`
		}
		if err := json.Unmarshal([]byte(value), &shape); err != nil {
			log.Printf(
				"wa_registration_wamsys_ga_shape kind=%s phone_hash=%s method=%s len=%d hash=%s parse_error=%s",
				probeLogValue(kind),
				phoneHash,
				probeLogValue(registrationMethodName(method, "sms")),
				len([]byte(value)),
				stableID(value),
				probeLogValue(err.Error()),
			)
			return
		}
		log.Printf(
			"wa_registration_wamsys_ga_shape kind=%s phone_hash=%s method=%s len=%d hash=%s bi_len=%d ap=%d ai=%d ae=%d mp=%t mu=%t",
			probeLogValue(kind),
			phoneHash,
			probeLogValue(registrationMethodName(method, "sms")),
			len([]byte(value)),
			stableID(value),
			len([]byte(shape.BootID)),
			shape.SourcePathAge,
			shape.DataPathAge,
			shape.ExternalPathAge,
			shape.MultiProcess,
			shape.MultiUser,
		)
		return
	}
}

func logNativeGPIAPlaintextShape(input wamsysMaterialInput, label string, keySource string, fields []nativeGPIAJSONField) {
	plaintext, err := renderNativeGPIAJSONObject(fields)
	if err != nil {
		log.Printf(
			"wa_registration_gpia_plaintext_shape kind=%s phone_hash=%s label=%s error=%s",
			probeLogValue(registrationRequestKindName(input.Kind)),
			wamsysInputPhoneHash(input),
			probeLogValue(label),
			probeLogValue(err.Error()),
		)
		return
	}
	log.Printf(
		"wa_registration_gpia_plaintext_shape kind=%s phone_hash=%s label=%s key_source_len=%d key_source_hash=%s json_len=%d json_hash=%s keys=%s fields=%s",
		probeLogValue(registrationRequestKindName(input.Kind)),
		wamsysInputPhoneHash(input),
		probeLogValue(label),
		len([]byte(keySource)),
		stableID(keySource),
		len(plaintext),
		stableID(string(plaintext)),
		probeLogValue(nativeGPIAFieldKeys(fields)),
		probeLogValue(nativeGPIAFieldShapes(fields)),
	)
}

func logNativeWamsysGAPlaintextShape(input wamsysMaterialInput, keySource string, bootIDMaterial string, fields []nativeGPIAJSONField) {
	plaintext, err := renderNativeGPIAJSONObject(fields)
	if err != nil {
		log.Printf(
			"wa_registration_wamsys_ga_plaintext_shape kind=%s phone_hash=%s error=%s",
			probeLogValue(registrationRequestKindName(input.Kind)),
			wamsysInputPhoneHash(input),
			probeLogValue(err.Error()),
		)
		return
	}
	log.Printf(
		"wa_registration_wamsys_ga_plaintext_shape kind=%s phone_hash=%s key_source_len=%d key_source_hash=%s boot_id_material_len=%d boot_id_material_hash=%s json_len=%d json_hash=%s keys=%s fields=%s",
		probeLogValue(registrationRequestKindName(input.Kind)),
		wamsysInputPhoneHash(input),
		len([]byte(keySource)),
		stableID(keySource),
		len([]byte(bootIDMaterial)),
		stableID(bootIDMaterial),
		len(plaintext),
		stableID(string(plaintext)),
		probeLogValue(nativeGPIAFieldKeys(fields)),
		probeLogValue(nativeGPIAFieldShapes(fields)),
	)
}

func nativeGPIAFieldKeys(fields []nativeGPIAJSONField) string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	return strings.Join(keys, "|")
}

func nativeGPIAFieldShapes(fields []nativeGPIAJSONField) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, nativeGPIAFieldShape(field))
	}
	return strings.Join(parts, ",")
}

func nativeGPIAFieldShape(field nativeGPIAJSONField) string {
	switch value := field.Value.(type) {
	case string:
		return field.Key + ":string:" + strconv.Itoa(len([]byte(value))) + ":" + stableID(value)
	case int:
		return field.Key + ":number:int:" + strconv.Itoa(value)
	case int64:
		return field.Key + ":number:int:" + strconv.FormatInt(value, 10)
	case bool:
		return field.Key + ":bool:" + strconv.FormatBool(value)
	case nil:
		return field.Key + ":null"
	default:
		return field.Key + ":type:" + probeLogValue(strconv.Quote(fmt.Sprintf("%T", value)))
	}
}

func wamsysInputPhoneHash(input wamsysMaterialInput) string {
	if input.Phone != nil && input.Phone.GetE164Number() != "" {
		return stableID(input.Phone.GetE164Number())
	}
	return ""
}

func registrationRequestKindName(kind waappv1.RegistrationRequestKind) string {
	switch kind {
	case waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST:
		return "exist"
	case waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE:
		return "code"
	case waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER:
		return "register"
	default:
		return kind.String()
	}
}

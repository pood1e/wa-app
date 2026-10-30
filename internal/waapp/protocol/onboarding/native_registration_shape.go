package onboarding

import (
	"encoding/json"
	"log"
	"net/url"
	"strconv"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func LogNativeRegistrationOrderedShape(kind string, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, params OrderedParams) {
	if len(params) == 0 {
		return
	}
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = shared.StableID(phone.GetE164Number())
	}
	log.Printf(
		"wa_registration_request_shape kind=%s phone_hash=%s method=%s field_count=%d fields=%s",
		shared.ProbeLogValue(kind),
		phoneHash,
		shared.ProbeLogValue(RegistrationMethodName(method, "sms")),
		len(params),
		RegistrationShapeFields(params),
	)
	LogNativeRegistrationValueHashes(kind, phoneHash, method, params)
	LogNativeRegistrationWamsysGAShape(kind, phoneHash, method, params)
}

func RegistrationShapeFields(params OrderedParams) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		mode := "form"
		if param.Raw {
			mode = "raw"
		}
		parts = append(parts, param.Key+":"+strconv.Itoa(RegistrationShapeValueLength(param.Val, param.Raw))+":"+mode)
	}
	return strings.Join(parts, ",")
}

func RegistrationShapeValueLength(value string, raw bool) int {
	if !raw {
		return len([]byte(value))
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return len([]byte(value))
	}
	return len([]byte(decoded))
}

func LogNativeRegistrationValueHashes(kind string, phoneHash string, method waappv1.VerificationDeliveryMethod, params OrderedParams) {
	values := RegistrationValueHashes(params)
	if values == "" {
		return
	}
	log.Printf(
		"wa_registration_value_hashes kind=%s phone_hash=%s method=%s values=%s",
		shared.ProbeLogValue(kind),
		phoneHash,
		shared.ProbeLogValue(RegistrationMethodName(method, "sms")),
		values,
	)
}

func RegistrationValueHashes(params OrderedParams) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		if !ShouldLogRegistrationValueHash(param.Key) {
			continue
		}
		value := RegistrationHashValue(param.Val, param.Raw)
		parts = append(parts, param.Key+":"+strconv.Itoa(len([]byte(value)))+":"+shared.StableID(value))
	}
	return strings.Join(parts, ",")
}

func RegistrationHashValue(value string, raw bool) string {
	if !raw {
		return value
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func ShouldLogRegistrationValueHash(key string) bool {
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

func LogNativeRegistrationWamsysGAShape(kind string, phoneHash string, method waappv1.VerificationDeliveryMethod, params OrderedParams) {
	for _, param := range params {
		if param.Key != "_ga" {
			continue
		}
		value := RegistrationHashValue(param.Val, param.Raw)
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
				shared.ProbeLogValue(kind),
				phoneHash,
				shared.ProbeLogValue(RegistrationMethodName(method, "sms")),
				len([]byte(value)),
				shared.StableID(value),
				shared.ProbeLogValue(err.Error()),
			)
			return
		}
		log.Printf(
			"wa_registration_wamsys_ga_shape kind=%s phone_hash=%s method=%s len=%d hash=%s bi_len=%d ap=%d ai=%d ae=%d mp=%t mu=%t",
			shared.ProbeLogValue(kind),
			phoneHash,
			shared.ProbeLogValue(RegistrationMethodName(method, "sms")),
			len([]byte(value)),
			shared.StableID(value),
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

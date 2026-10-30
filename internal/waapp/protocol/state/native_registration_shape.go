package state

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func LogNativeGPIAPlaintextShape(input WamsysMaterialInput, label string, keySource string, fields []NativeGPIAJSONField) {
	plaintext, err := RenderNativeGPIAJSONObject(fields)
	if err != nil {
		log.Printf(
			"wa_registration_gpia_plaintext_shape kind=%s phone_hash=%s label=%s error=%s",
			shared.ProbeLogValue(RegistrationRequestKindName(input.Kind)),
			WamsysInputPhoneHash(input),
			shared.ProbeLogValue(label),
			shared.ProbeLogValue(err.Error()),
		)
		return
	}
	jsonHash := shared.StableID(string(plaintext))
	if NativeGPIAFieldsContainSensitive(fields) {
		jsonHash = "sensitive"
	}
	log.Printf(
		"wa_registration_gpia_plaintext_shape kind=%s phone_hash=%s label=%s key_source_len=%d key_source_hash=%s json_len=%d json_hash=%s keys=%s fields=%s",
		shared.ProbeLogValue(RegistrationRequestKindName(input.Kind)),
		WamsysInputPhoneHash(input),
		shared.ProbeLogValue(label),
		len([]byte(keySource)),
		shared.StableID(keySource),
		len(plaintext),
		jsonHash,
		shared.ProbeLogValue(NativeGPIAFieldKeys(fields)),
		shared.ProbeLogValue(NativeGPIAFieldShapes(fields)),
	)
}

func LogNativeWamsysGAPlaintextShape(input WamsysMaterialInput, keySource string, bootIDMaterial string, fields []NativeGPIAJSONField) {
	plaintext, err := RenderNativeGPIAJSONObject(fields)
	if err != nil {
		log.Printf(
			"wa_registration_wamsys_ga_plaintext_shape kind=%s phone_hash=%s error=%s",
			shared.ProbeLogValue(RegistrationRequestKindName(input.Kind)),
			WamsysInputPhoneHash(input),
			shared.ProbeLogValue(err.Error()),
		)
		return
	}
	log.Printf(
		"wa_registration_wamsys_ga_plaintext_shape kind=%s phone_hash=%s key_source_len=%d key_source_hash=%s boot_id_material_len=%d boot_id_material_hash=%s json_len=%d json_hash=%s keys=%s fields=%s",
		shared.ProbeLogValue(RegistrationRequestKindName(input.Kind)),
		WamsysInputPhoneHash(input),
		len([]byte(keySource)),
		shared.StableID(keySource),
		len([]byte(bootIDMaterial)),
		shared.StableID(bootIDMaterial),
		len(plaintext),
		shared.StableID(string(plaintext)),
		shared.ProbeLogValue(NativeGPIAFieldKeys(fields)),
		shared.ProbeLogValue(NativeGPIAFieldShapes(fields)),
	)
}

func NativeGPIAFieldKeys(fields []NativeGPIAJSONField) string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	return strings.Join(keys, "|")
}

func NativeGPIAFieldShapes(fields []NativeGPIAJSONField) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, NativeGPIAFieldShape(field))
	}
	return strings.Join(parts, ",")
}

func NativeGPIAFieldShape(field NativeGPIAJSONField) string {
	switch value := field.Value.(type) {
	case string:
		if NativeGPIAFieldSensitive(field.Key) {
			return field.Key + ":string:" + strconv.Itoa(len([]byte(value))) + ":sensitive"
		}
		return field.Key + ":string:" + strconv.Itoa(len([]byte(value))) + ":" + shared.StableID(value)
	case int:
		return field.Key + ":number:int:" + strconv.Itoa(value)
	case int64:
		return field.Key + ":number:int:" + strconv.FormatInt(value, 10)
	case bool:
		return field.Key + ":bool:" + strconv.FormatBool(value)
	case nil:
		return field.Key + ":null"
	default:
		return field.Key + ":type:" + shared.ProbeLogValue(strconv.Quote(fmt.Sprintf("%T", value)))
	}
}

func NativeGPIAFieldSensitive(key string) bool {
	switch key {
	case "token", "_it":
		return true
	default:
		return false
	}
}

func NativeGPIAFieldsContainSensitive(fields []NativeGPIAJSONField) bool {
	for _, field := range fields {
		if NativeGPIAFieldSensitive(field.Key) {
			return true
		}
	}
	return false
}

func WamsysInputPhoneHash(input WamsysMaterialInput) string {
	if input.Phone != nil && input.Phone.GetE164Number() != "" {
		return shared.StableID(input.Phone.GetE164Number())
	}
	return ""
}

func RegistrationRequestKindName(kind protocolv1.RegistrationRequestKind) string {
	switch kind {
	case protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST:
		return "exist"
	case protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE:
		return "code"
	case protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER:
		return "register"
	default:
		return kind.String()
	}
}

package state

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

var nativeUserAgentUnsafe = regexp.MustCompile(`[^,.\w()-]`)

func NativeUserAgentForProfile(profile NativePhoneProfile, appVersion string) string {
	return "WhatsApp/" + strings.ReplaceAll(NativeAppVersion(appVersion), " ", "_") + " " + NativeDeviceUserAgent(profile)
}

func NativeDeviceUserAgent(profile NativePhoneProfile) string {
	profile = NormalizeNativePhoneProfile(profile)
	return "Android/" + nativeUserAgentUnsafe.ReplaceAllString(profile.AndroidVersion, "_") +
		" Device/" + nativeUserAgentUnsafe.ReplaceAllString(profile.DeviceVendor, "_") +
		"-" + nativeUserAgentUnsafe.ReplaceAllString(profile.DeviceModel, "_")
}

func NativeAppVersion(appVersion string) string {
	if strings.TrimSpace(appVersion) == "" {
		return pwire.DefaultWAAppVersion
	}
	return strings.TrimSpace(appVersion)
}

func NormalizeNativePhoneProfile(profile NativePhoneProfile) NativePhoneProfile {
	if ShouldNormalizeNativeAccessSessionID(profile) {
		profile = NormalizeNativeAccessSessionID(profile)
	}
	if len(profile.AdditionalMapFields) > 0 {
		fields := make(map[string]string, len(profile.AdditionalMapFields))
		for key, value := range profile.AdditionalMapFields {
			if IsRuntimeNativeDeviceMapKey(key) {
				continue
			}
			fields[key] = value
		}
		profile.AdditionalMapFields = fields
	}
	return profile
}

func ShouldNormalizeNativeAccessSessionID(profile NativePhoneProfile) bool {
	return profile.AccessSessionID != "" ||
		profile.AccessSessionIDUUID != "" ||
		profile.FDID != "" ||
		profile.PhoneSHA256 != ""
}

func NormalizeNativeAccessSessionID(profile NativePhoneProfile) NativePhoneProfile {
	if IsUUIDText(profile.AccessSessionIDUUID) {
		profile.AccessSessionID = NativeUUIDTextToB64u(profile.AccessSessionIDUUID)
		return profile
	}
	if IsUUIDText(profile.AccessSessionID) {
		profile.AccessSessionIDUUID = profile.AccessSessionID
		profile.AccessSessionID = NativeUUIDTextToB64u(profile.AccessSessionID)
		return profile
	}
	if accessSessionIDUUID, ok := NativeUUIDB64uToText(profile.AccessSessionID); ok {
		profile.AccessSessionIDUUID = accessSessionIDUUID
		return profile
	}
	accessSessionIDUUID, accessSessionID := UuidPair()
	profile.AccessSessionID = accessSessionID
	profile.AccessSessionIDUUID = accessSessionIDUUID
	return profile
}

func NativeUUIDTextToB64u(value string) string {
	value = strings.TrimSpace(value)
	if !IsUUIDText(value) {
		return ""
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 {
		return ""
	}
	return pwire.B64u(raw)
}

func NativeUUIDB64uToText(value string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != 16 {
		return "", false
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), true
}

func IsUUIDText(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for idx, ch := range value {
		switch idx {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func ResponseStatus(data map[string]any) string {
	if value, ok := data["status"].(string); ok {
		return strings.ToLower(value)
	}
	return ""
}

func FullPhoneKey(cc string, phone string) string {
	compact := regexp.MustCompile(`\D+`).ReplaceAllString(cc+phone, "")
	if compact == "" {
		return hex.EncodeToString(sha256.New().Sum(nil))
	}
	return compact
}

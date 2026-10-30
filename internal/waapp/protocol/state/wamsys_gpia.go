package state

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

const NativeGPIAPackageName = "com.whatsapp"

type NativeGPIAMaterial struct {
	Primary       string
	CodeCompact   string
	DeviceCompact string
}

type NativeGPIAJSONField struct {
	Key   string
	Value any
}

func BuildNativeGPIAMaterial(input WamsysMaterialInput) (NativeGPIAMaterial, error) {
	sourceDir := NativeGPIASourceDir(input)
	keySource := NativeGPIAKeySource(input.State)
	if input.Integrity == nil {
		return NativeGPIAMaterial{}, fmt.Errorf("Play Integrity outcome is required")
	}
	material := input.Build.GetApplicationIntegrity()
	if material == nil {
		return NativeGPIAMaterial{}, fmt.Errorf("application integrity material is required")
	}
	var token string
	var errorCode int
	switch outcome := input.Integrity.GetOutcome().(type) {
	case *waappv1.AndroidIntegrityResult_Token:
		if outcome.Token == "" || len(outcome.Token) > 16384 {
			return NativeGPIAMaterial{}, fmt.Errorf("invalid Play Integrity token")
		}
		token = outcome.Token
	case *waappv1.AndroidIntegrityResult_ErrorCode:
		if outcome.ErrorCode == 0 {
			return NativeGPIAMaterial{}, fmt.Errorf("invalid Play Integrity error code")
		}
		errorCode = int(outcome.ErrorCode)
	default:
		return NativeGPIAMaterial{}, fmt.Errorf("Play Integrity outcome is required")
	}
	primaryFields := []NativeGPIAJSONField{
		{Key: "sizeInBytes", Value: strconv.FormatUint(material.GetApkSize(), 10)},
		{Key: "packageName", Value: NativeGPIAPackageName},
	}
	if token != "" {
		primaryFields = append(primaryFields, NativeGPIAJSONField{Key: "token", Value: token})
	}
	primaryFields = append(primaryFields, []NativeGPIAJSONField{
		{Key: "code", Value: errorCode},
		{Key: "shatr", Value: B64Std(material.GetApkPrefixSha256())},
		{Key: "p", Value: sourceDir},
		{Key: "cert", Value: B64Std(material.GetCertificateSha1())},
		{Key: "sha256", Value: B64Std(material.GetApkSha256())},
	}...)
	LogNativeGPIAPlaintextShape(input, "primary_long", keySource, primaryFields)
	primary, err := EncryptNativeGPIAJSON(keySource, primaryFields)
	if err != nil {
		return NativeGPIAMaterial{}, err
	}
	codeCompactFields := []NativeGPIAJSONField{}
	if token != "" {
		codeCompactFields = append(codeCompactFields, NativeGPIAJSONField{Key: "_it", Value: token})
	}
	codeCompactFields = append(codeCompactFields, NativeGPIAJSONField{Key: "_ic", Value: errorCode})
	LogNativeGPIAPlaintextShape(input, "token_compact", keySource, codeCompactFields)
	codeCompact, err := EncryptNativeGPIAJSON(keySource, codeCompactFields)
	if err != nil {
		return NativeGPIAMaterial{}, err
	}
	deviceCompactFields := NativeGPIADeviceCompactFields(input, sourceDir)
	LogNativeGPIAPlaintextShape(input, "device_compact", keySource, deviceCompactFields)
	deviceCompact, err := EncryptNativeGPIAJSON(keySource, deviceCompactFields)
	if err != nil {
		return NativeGPIAMaterial{}, err
	}
	return NativeGPIAMaterial{Primary: primary, CodeCompact: codeCompact, DeviceCompact: deviceCompact}, nil
}

func NativeGPIADeviceCompactFields(input WamsysMaterialInput, sourceDir string) []NativeGPIAJSONField {
	material := input.Build.GetApplicationIntegrity()
	return []NativeGPIAJSONField{
		{Key: "_dh", Value: B64Std(material.GetPrimaryDexSha256())},
		{Key: "_iln", Value: B64Std(input.Client.GetDataSoNamesSha256())},
		{Key: "_isb", Value: strconv.FormatUint(material.GetApkSize(), 10)},
		{Key: "_ip", Value: NativeGPIAPackageName},
		{Key: "did", Value: NativeGPIADisplayID(input.State)},
		{Key: "_p", Value: sourceDir},
		{Key: "_ln", Value: B64Std(input.Client.GetNativeLibraryNamesSha256())},
		{Key: "_ist", Value: B64Std(material.GetApkPrefixSha256())},
		{Key: "_icr", Value: B64Std(material.GetCertificateSha1())},
		{Key: "_is", Value: B64Std(material.GetApkSha256())},
	}
}

func NativeGPIADisplayID(state NativeState) string {
	return state.Profile.BuildDisplayID
}

func NativeGPIASourceDir(input WamsysMaterialInput) string {
	return NativeStableGPIASourceDir(input.State)
}

func NativeStableGPIASourceDir(state NativeState) string {
	first := NativeStableInstallToken(state, "source-dir-prefix")
	second := NativeStableInstallToken(state, "source-dir-package")
	return "/data/app/~~" + first + "==/com.whatsapp-" + second + "==/base.apk"
}

func NativeStableInstallToken(state NativeState, label string) string {
	sum := sha256.Sum256([]byte(NativeStableRuntimeSeed(state, label)))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func NativeGPIAKeySource(state NativeState) string {
	if private, err := state.ChatStatic.PrivateBytes(); err == nil && len(private) == curve25519.ScalarSize {
		if public, err := curve25519.X25519(private, curve25519.Basepoint); err == nil {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	for _, candidate := range []string{state.AuthKey, state.ChatStatic.Public} {
		public, err := pwire.DecodeB64Any(candidate)
		if err == nil && len(public) == curve25519.PointSize {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	return ""
}

func EncryptNativeGPIAJSON(keySource string, fields []NativeGPIAJSONField) (string, error) {
	plaintext, err := RenderNativeGPIAJSONObject(fields)
	if err != nil {
		return "", err
	}
	return EncryptNativeGPIAData(keySource, plaintext)
}

func EncryptNativeGPIAData(keySource string, plaintext []byte) (string, error) {
	if keySource == "" {
		return "", fmt.Errorf("WAMSYS binding key is required")
	}
	key := sha256.Sum256([]byte(keySource))
	iv := pwire.RandomBytes(aes.BlockSize)
	ciphertext, err := pwire.AesCBCPKCS7Encrypt(plaintext, key[:], iv)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(iv)+len(ciphertext))
	out = append(out, iv...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func RenderNativeGPIAJSONObject(fields []NativeGPIAJSONField) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(RenderNativeGPIAJSONString(field.Key))
		b.WriteByte(':')
		value, err := RenderNativeGPIAJSONValue(field.Value)
		if err != nil {
			return nil, err
		}
		b.Write(value)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func RenderNativeGPIAJSONValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return RenderNativeGPIAJSONString(v), nil
	case int:
		return []byte(strconv.Itoa(v)), nil
	case int64:
		return []byte(strconv.FormatInt(v, 10)), nil
	case bool:
		return []byte(strconv.FormatBool(v)), nil
	case nil:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("unsupported native GPIA JSON value type %T", value)
	}
}

func RenderNativeGPIAJSONString(value string) []byte {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"', '\\', '/':
			b.WriteByte('\\')
			b.WriteRune(char)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if char <= 0x1f {
				_, _ = fmt.Fprintf(&b, `\u%04x`, char)
				continue
			}
			b.WriteRune(char)
		}
	}
	b.WriteByte('"')
	return []byte(b.String())
}

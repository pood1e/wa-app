package app

import (
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"
)

const (
	nativeGPIAPackageName  = "com.whatsapp"
	nativeGPIASourceSize   = "141896808"
	nativeGPIASourceDigest = "Osq4rcTiHZAOGoPRfEuPX9fBX5w+IanRQ3Rczay4yHE="
	// Full app-release APK SHA-256/Base64; native bootstrap stores this in
	// global 0xc45a48 for GPIA sha256/_is.
	nativeGPIASourceFullDigest = "l+Cxm/2+TxcFMB2bKnIDlwIgk2YUgiUnhGYws9XjCW0="
	nativeGPIACertDigest       = "OKD31QX+GP7GT780Psqq8xDb15k="
	nativeGPIAClassesDigest    = "x4woWJaRyXusuP+MRZNlKP9q/zi9TXPPdwkZpEoKVeU="
	nativeGPIANativeLibDigest  = "KMr1FDZ5Qv9UsYvUwaPmFmshuABXLq3rfxeELvAebKk="
	nativeGPIADataSODigest     = "/2slt0vplE6OE7wMz/C41mG1HvIdraHa5P/RB1MWGW0="
)

// nativeGPIAErrorCodePool 是 GPIA 错误材料 code/_ic 可轮换的 Play Integrity 错误码,均为"设备本就
// 做不了 Integrity"的稳定设备态(对照 APK com.google.android.play.core.integrity.model):
//   -2 PLAY_STORE_NOT_FOUND / -6 PLAY_SERVICES_NOT_FOUND / -1 API_NOT_AVAILABLE /
//   -4 PLAY_STORE_ACCOUNT_NOT_FOUND。
// 刻意只收稳定态:瞬时码(-3/-12/-18 等)隐含"重试该成功",作每账号持久值不自洽;
// 篡改/限流/配置错误码(-7/-8/-10/-11/-13/-16/-17/-19)不可用。每账号按稳定种子确定性选一个,
// 与设备指纹一样在注册期固定,呈现"这台无 GMS 设备"的一致画像。
var nativeGPIAErrorCodePool = []int{-2, -6, -1, -4}

// nativeGPIAErrorCode 按账号稳定种子从池中确定性选取错误码:同账号每次构造一致,跨账号分散。
func nativeGPIAErrorCode(state nativeState) int {
	pool := nativeGPIAErrorCodePool
	sum := sha256.Sum256([]byte(nativeStableRuntimeSeed(state, "gpia-error-code")))
	return pool[int(sum[0])%len(pool)]
}

type nativeGPIAMaterial struct {
	Primary       string
	CodeCompact   string
	DeviceCompact string
}

type nativeGPIAJSONField struct {
	Key   string
	Value any
}

func buildNativeGPIAErrorMaterial(input wamsysMaterialInput) (nativeGPIAMaterial, error) {
	sourceDir := nativeGPIASourceDir(input)
	keySource := nativeGPIAKeySource(input.State)
	errorCode := nativeGPIAErrorCode(input.State)
	primaryFields := []nativeGPIAJSONField{
		{Key: "sizeInBytes", Value: nativeGPIASourceSize},
		{Key: "packageName", Value: nativeGPIAPackageName},
		{Key: "code", Value: errorCode},
		{Key: "shatr", Value: nativeGPIASourceDigest},
		{Key: "p", Value: sourceDir},
		{Key: "cert", Value: nativeGPIACertDigest},
		{Key: "sha256", Value: nativeGPIASourceFullDigest},
	}
	logNativeGPIAPlaintextShape(input, "primary_long", keySource, primaryFields)
	primary, err := encryptNativeGPIAJSON(keySource, primaryFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	codeCompactFields := []nativeGPIAJSONField{
		{Key: "_ic", Value: errorCode},
	}
	logNativeGPIAPlaintextShape(input, "token_compact", keySource, codeCompactFields)
	codeCompact, err := encryptNativeGPIAJSON(keySource, codeCompactFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	deviceCompactFields := []nativeGPIAJSONField{
		{Key: "_dh", Value: nativeGPIAClassesDigest},
		{Key: "_iln", Value: nativeGPIADataSODigest},
		{Key: "_isb", Value: nativeGPIASourceSize},
		{Key: "_ip", Value: nativeGPIAPackageName},
		{Key: "did", Value: nativeGPIADisplayID(input.State)},
		{Key: "_p", Value: sourceDir},
		{Key: "_ln", Value: nativeGPIANativeLibDigest},
		{Key: "_ist", Value: nativeGPIASourceDigest},
		{Key: "_icr", Value: nativeGPIACertDigest},
		{Key: "_is", Value: nativeGPIASourceFullDigest},
	}
	logNativeGPIAPlaintextShape(input, "device_compact", keySource, deviceCompactFields)
	deviceCompact, err := encryptNativeGPIAJSON(keySource, deviceCompactFields)
	if err != nil {
		return nativeGPIAMaterial{}, err
	}
	return nativeGPIAMaterial{Primary: primary, CodeCompact: codeCompact, DeviceCompact: deviceCompact}, nil
}

func nativeGPIADisplayID(state nativeState) string {
	profile := normalizeNativePhoneProfile(state.Profile, "")
	return firstNonEmpty(profile.BuildDisplayID, defaultNativeDeviceModel().BuildDisplayID)
}

func nativeGPIASourceDir(input wamsysMaterialInput) string {
	return nativeStableGPIASourceDir(input.State)
}

func nativeStableGPIASourceDir(state nativeState) string {
	first := nativeStableInstallToken(state, "source-dir-prefix")
	second := nativeStableInstallToken(state, "source-dir-package")
	return "/data/app/~~" + first + "==/com.whatsapp-" + second + "==/base.apk"
}

func nativeStableInstallToken(state nativeState, label string) string {
	sum := sha256.Sum256([]byte(nativeStableRuntimeSeed(state, label)))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func nativeGPIAKeySource(state nativeState) string {
	if private, err := state.ChatStatic.privateBytes(); err == nil && len(private) == curve25519.ScalarSize {
		if public, err := curve25519.X25519(private, curve25519.Basepoint); err == nil {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	for _, candidate := range []string{state.AuthKey, state.ChatStatic.Public} {
		public, err := decodeB64Any(candidate)
		if err == nil && len(public) == curve25519.PointSize {
			return base64.StdEncoding.EncodeToString(public)
		}
	}
	return "default"
}

func encryptNativeGPIAJSON(keySource string, fields []nativeGPIAJSONField) (string, error) {
	plaintext, err := renderNativeGPIAJSONObject(fields)
	if err != nil {
		return "", err
	}
	return encryptNativeGPIAData(keySource, plaintext)
}

func encryptNativeGPIAData(keySource string, plaintext []byte) (string, error) {
	key := sha256.Sum256([]byte(keySource))
	iv := randomBytes(aes.BlockSize)
	ciphertext, err := aesCBCPKCS7Encrypt(plaintext, key[:], iv)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(iv)+len(ciphertext))
	out = append(out, iv...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func renderNativeGPIAJSONObject(fields []nativeGPIAJSONField) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(renderNativeGPIAJSONString(field.Key))
		b.WriteByte(':')
		value, err := renderNativeGPIAJSONValue(field.Value)
		if err != nil {
			return nil, err
		}
		b.Write(value)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func renderNativeGPIAJSONValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return renderNativeGPIAJSONString(v), nil
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

func renderNativeGPIAJSONString(value string) []byte {
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

package wire

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

type UserAgentConfig struct {
	Platform      protocolv1.ClientPlatform
	Version       string
	OsVersion     string
	Manufacturer  string
	Device        string
	OsBuildNumber string
	PhoneID       string
	LocaleLang    string
	LocaleCountry string
	DeviceBoard   string
	DeviceExpID   string
	DeviceType    uint64
	ModelType     string
	Mcc           string
	Mnc           string
}

type LoginPayloadConfig struct {
	SessionID           uint64
	Passive             bool
	PushName            string
	ShortConnect        bool
	ConnectType         uint64
	ConnectReason       uint64
	ConnectAttemptCount uint64
	DeviceID            *uint64
	Product             uint64
	Oc                  bool
	Lc                  uint64
}

func ParseVersion(version string) [5]uint64 {
	var out [5]uint64
	parts := strings.Split(version, ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		var value uint64
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			value = value*10 + uint64(r-'0')
		}
		out[i] = value
	}
	return out
}

func BuildAppVersion(version string) []byte {
	v := ParseVersion(version)
	out := []byte{}
	for i, value := range v {
		out = append(out, PbVarintField(i+1, value)...)
	}
	return out
}

func BuildUserAgentPayload(cfg UserAgentConfig) []byte {
	out := []byte{}
	platform := uint64(0)
	if cfg.Platform == protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS {
		platform = 1
	}
	out = append(out, PbVarintField(1, platform)...)
	out = append(out, PbBytesField(2, BuildAppVersion(cfg.Version))...)
	out = append(out, PbStringField(3, cfg.Mcc)...)
	out = append(out, PbStringField(4, cfg.Mnc)...)
	out = append(out, PbStringField(5, cfg.OsVersion)...)
	out = append(out, PbStringField(6, cfg.Manufacturer)...)
	out = append(out, PbStringField(7, cfg.Device)...)
	out = append(out, PbStringField(8, cfg.OsBuildNumber)...)
	out = append(out, PbStringField(9, cfg.PhoneID)...)
	out = append(out, PbVarintField(10, 0)...)
	out = append(out, PbStringField(11, cfg.LocaleLang)...)
	out = append(out, PbStringField(12, cfg.LocaleCountry)...)
	out = append(out, PbStringField(13, cfg.DeviceBoard)...)
	out = append(out, PbStringField(14, cfg.DeviceExpID)...)
	out = append(out, PbVarintField(15, cfg.DeviceType)...)
	out = append(out, PbStringField(16, cfg.ModelType)...)
	return out
}

func BuildLoginPayload(identity LoginIdentity, ua UserAgentConfig, cfg LoginPayloadConfig) []byte {
	out := []byte{}
	out = append(out, PbVarintField(1, identity.Username)...)
	out = append(out, PbBoolField(3, cfg.Passive)...)
	out = append(out, PbBytesField(5, BuildUserAgentPayload(ua))...)
	out = append(out, PbStringField(7, cfg.PushName)...)
	out = append(out, PbVarintField(9, cfg.SessionID)...)
	out = append(out, PbBoolField(10, cfg.ShortConnect)...)
	out = append(out, PbVarintField(12, cfg.ConnectType)...)
	out = append(out, PbVarintField(13, cfg.ConnectReason)...)
	out = append(out, PbVarintField(16, cfg.ConnectAttemptCount)...)
	if cfg.DeviceID != nil {
		out = append(out, PbVarintField(18, *cfg.DeviceID)...)
	}
	out = append(out, PbVarintField(20, cfg.Product)...)
	out = append(out, PbBoolField(23, cfg.Oc)...)
	out = append(out, PbVarintField(24, cfg.Lc)...)
	return out
}

func CompressMaybeDecodeNodePayload(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, NewChatdError("empty decrypted frame")
	}
	if len(plaintext) == 1 {
		return nil, NewChatdError("header-only decrypted frame")
	}
	header := plaintext[0]
	body := plaintext[1:]
	if header&2 != 0 {
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		decompressed, err := io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			return nil, err
		}
		body = decompressed
	}
	if header&1 != 0 {
		return nil, NewChatdError("fragmented stanza is not supported")
	}
	return body, nil
}

func WaitDeadline(timeout time.Duration) time.Time {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return time.Now().Add(timeout)
}

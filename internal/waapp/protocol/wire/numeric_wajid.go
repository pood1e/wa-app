package wire

import (
	"bytes"
	"compress/gzip"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func NumericWAJID(value uint64, domain string) string {
	if value == 0 {
		return ""
	}
	local := strconv.FormatUint(value, 10)
	if len(local) < 5 || len(local) > 24 {
		return ""
	}
	return local + "@" + domain
}

func PhoneNumberWAJID(value string) string {
	digits := shared.DigitsOnly(value)
	if len(digits) < 5 || len(digits) > 24 {
		return ""
	}
	return digits + "@s.whatsapp.net"
}

func WaGunzipPayload(raw []byte) ([]byte, bool) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return nil, false
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, WaContactDecodedPayloadLimit+1))
	if err != nil || len(data) == 0 || len(data) > WaContactDecodedPayloadLimit {
		return nil, false
	}
	return data, true
}

func WaProtoPlainString(raw []byte) string {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.ContainsRune(text, 0) {
		return ""
	}
	return text
}

func WaContactNameString(raw []byte) string {
	return wacore.WAContactName(WaProtoPlainString(raw))
}

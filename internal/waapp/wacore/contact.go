package wacore

import (
	"strings"
	"unicode/utf8"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

// WAContactHint is a normalized identity hint extracted from WhatsApp protocol
// payloads (LID/PN JIDs plus display/verified names). It is a pure value type;
// its JSON tags are part of the frozen nativeState on-disk schema.
type WAContactHint struct {
	LIDJID       string `json:"lid_jid,omitempty"`
	PNJID        string `json:"pn_jid,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	WAName       string `json:"wa_name,omitempty"`
	Username     string `json:"username,omitempty"`
	VerifiedName string `json:"verified_name,omitempty"`
}

func (h WAContactHint) Valid() bool {
	h = h.Normalized()
	return h.LIDJID != "" && (h.PNJID != "" || h.DisplayName != "" || h.WAName != "" || h.Username != "" || h.VerifiedName != "")
}

func (h WAContactHint) Normalized() WAContactHint {
	h.LIDJID = NormalizeWAJID(h.LIDJID)
	h.PNJID = NormalizeWAJID(h.PNJID)
	h.DisplayName = WAContactName(h.DisplayName)
	h.WAName = WAContactName(h.WAName)
	h.Username = WAContactName(h.Username)
	h.VerifiedName = WAContactName(h.VerifiedName)
	if h.LIDJID != "" && !strings.HasSuffix(h.LIDJID, "@lid") {
		h.LIDJID = ""
	}
	if h.PNJID != "" && !strings.HasSuffix(h.PNJID, "@s.whatsapp.net") {
		h.PNJID = ""
	}
	return h
}

// NormalizeWAJID canonicalizes a contact reference into a WA JID: bare digit
// strings become "<digits>@s.whatsapp.net", already-qualified refs pass through,
// and empty/bare-domain refs become "".
func NormalizeWAJID(ref string) string {
	value := strings.TrimSpace(ref)
	if value == "" || value == "s.whatsapp.net" {
		return ""
	}
	if strings.Contains(value, "@") {
		return value
	}
	digits := shared.DigitsOnly(value)
	if digits != "" && digits == value {
		return digits + "@s.whatsapp.net"
	}
	return value
}

// WAContactName trims and validates a human contact name, capping it at 120
// runes and rejecting non-UTF-8 / NUL-bearing input.
func WAContactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return ""
	}
	return waproto.TrimWARunes(value, 120)
}

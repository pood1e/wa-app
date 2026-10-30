package wamodel

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const ContactMessagePreviewLimit = 96

func ContactMessagePreview(plaintext string, redacted string, payloadRef string, state waappv1.MessageEncryptionState) string {
	value := shared.FirstNonEmpty(plaintext, redacted, PayloadTextSummary(payloadRef), MessageStatePreview(state))
	return TruncatePreview(value, ContactMessagePreviewLimit)
}

func TruncatePreview(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func MessageStatePreview(state waappv1.MessageEncryptionState) string {
	switch state {
	case waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED:
		return "消息待解密"
	case waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED:
		return "消息解密失败"
	default:
		return ""
	}
}

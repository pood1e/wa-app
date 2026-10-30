package communication

import (
	"log"
	"strconv"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func logAccountSecurityIQ(kind waappv1.AccountSettingsOperationKind, platform protocolv1.ClientPlatform, response wire.ChatdNode) {
	switch kind {
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_STATUS_GET,
		waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_SETTINGS,
		waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET,
		waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST,
		waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY:
	default:
		return
	}
	errorNode, _ := ChatdChild(response, "error")
	errorCode, _ := strconv.ParseUint(errorNode.Attrs["code"], 10, 16)
	tags := make([]string, 0, 4)
	for _, child := range wire.ChatdChildren(response) {
		switch child.Tag {
		case "2fa", "email", "verify_email", "error":
			tags = append(tags, child.Tag)
		}
	}
	log.Printf("WA account security iq kind=%s platform=%s type=%s children=%s error_code=%d error_type=%s error_reason=%s",
		kind, platform, accountSecurityIQType(response.Attrs["type"]), strings.Join(tags, ","), errorCode,
		accountSecurityErrorToken(errorNode.Attrs["type"]), accountSecurityErrorToken(errorNode.Attrs["text"]))
}

func accountSecurityIQType(value string) string {
	if value == "result" || value == "error" {
		return value
	}
	return "unknown"
}

func accountSecurityErrorToken(value string) string {
	switch value {
	case "cancel", "auth", "modify", "wait", "not-allowed", "not-authorized", "not-acceptable", "bad-request", "forbidden", "conflict", "resource-constraint", "service-unavailable", "internal-server-error":
		return value
	default:
		return "unknown"
	}
}

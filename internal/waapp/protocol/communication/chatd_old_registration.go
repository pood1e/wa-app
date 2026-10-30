package communication

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type ChatdOldRegistrationOTP struct {
	Code      string
	DeviceID  string
	ExpiresAt time.Time
}

func ChatdDeviceIDFromState(state pstate.NativeState) string {
	return ChatdDeviceIDFromUUID(state.Client.GetPhoneId())
}

func ChatdDeviceIDFromUUID(value string) string {
	compact := strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	raw, err := hex.DecodeString(compact)
	if err != nil || len(raw) != 16 {
		return ""
	}
	return pwire.B64u(raw)
}

func OldRegistrationOTPFromChatdNode(node pwire.ChatdNode, currentDeviceID string, now time.Time) (ChatdOldRegistrationOTP, bool, bool) {
	if node.Tag != "notification" {
		return ChatdOldRegistrationOTP{}, false, false
	}
	child, ok := ChatdChild(node, "wa_old_registration")
	if !ok {
		return ChatdOldRegistrationOTP{}, false, false
	}
	otp := ChatdOldRegistrationOTP{
		Code:      strings.TrimSpace(child.Attrs["code"]),
		DeviceID:  strings.TrimSpace(child.Attrs["device_id"]),
		ExpiresAt: OldRegistrationExpiresAt(child.Attrs["expiry_t"]),
	}
	if otp.Code == "" || otp.ExpiresAt.IsZero() || !now.Before(otp.ExpiresAt) {
		return otp, true, false
	}
	if currentDeviceID != "" && otp.DeviceID == currentDeviceID {
		return otp, true, false
	}
	return otp, true, true
}

func OldRegistrationExpiresAt(value string) time.Time {
	stamp, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || stamp <= 0 {
		return time.Time{}
	}
	if stamp > 1_000_000_000_000 {
		return time.UnixMilli(stamp).UTC()
	}
	return time.Unix(stamp, 0).UTC()
}

func OldRegistrationOTPMessage(input wacore.EngineMessageInput, node pwire.ChatdNode, otp ChatdOldRegistrationOTP, now time.Time) *waappv1.OtpMessage {
	if strings.TrimSpace(input.WAAccountID) == "" || strings.TrimSpace(otp.Code) == "" || otp.ExpiresAt.IsZero() {
		return nil
	}
	otpID := "waotp_old_" + shared.StableID(strings.Join([]string{input.WAAccountID, node.Attrs["id"], otp.Code, strconv.FormatInt(otp.ExpiresAt.Unix(), 10)}, ":"))
	return &waappv1.OtpMessage{
		OtpMessageId:         otpID,
		WaAccountId:          input.WAAccountID,
		ClientProfileId:      input.ClientProfileID,
		RegisteredIdentityId: input.RegisteredIdentityID,
		Source:               waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION,
		SourceParty:          shared.FirstNonEmpty(node.Attrs["from"], "wa_old_registration"),
		Otp:                  &waappv1.SensitiveText{Value: otp.Code, RedactedValue: shared.Redacted(otp.Code), SecretRef: "wa-otp:" + shared.StableID(otpID)},
		ReceivedAt:           timestamppb.New(now.UTC()),
		ExpiresAt:            timestamppb.New(otp.ExpiresAt.UTC()),
	}
}

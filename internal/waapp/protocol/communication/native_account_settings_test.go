package communication

import (
	"bytes"
	"strings"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func TestAccountSecurityRequestsUseNativeBinaryBodies(t *testing.T) {
	for _, test := range []struct {
		name, method, parent, field, value string
		input                              wacore.EngineAccountSettingsInput
	}{
		{"email_otp", "get", "verify_email", "code", "001234", wacore.EngineAccountSettingsInput{Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY, Code: "001234"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := wire.NewBinaryNodeCodec().EncodeNode(BuildAccountSettingsIQ("fixture", test.input))
			if err != nil {
				t.Fatal(err)
			}
			node, err := waBinary.Unmarshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if node.Tag != "iq" || node.Attrs["type"] != test.method || node.Attrs["xmlns"] != "urn:xmpp:whatsapp:account" || node.Attrs["to"] != types.ServerJID {
				t.Fatal("account security IQ routing differs from native clients")
			}
			parent := node.GetChildByTag(test.parent)
			content := parent.GetChildByTag(test.field).Content
			data, ok := content.([]byte)
			if !ok || !bytes.Equal(data, []byte(test.value)) {
				t.Fatalf("native security code must decode as bytes with leading zeros preserved, got %T", content)
			}
		})
	}
}

func TestEmailVerificationRejectsIncompleteSuccessResponses(t *testing.T) {
	for _, input := range []wacore.EngineAccountSettingsInput{
		{Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET, EmailAddress: "fixture@example.com"},
		{Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST},
		{Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY},
	} {
		t.Run(input.Kind.String(), func(t *testing.T) {
			result := AccountSettingsResultFromIQ(input, wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}})
			if result.Err == nil || result.Status != waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED || result.TwoFactorStatus != nil {
				t.Fatal("incomplete response changed account security state")
			}
		})
	}
}

func TestEmailVerificationRequiresExplicitNativeResults(t *testing.T) {
	setting := wire.ChatdNode{Tag: "iq", Content: []wire.ChatdNode{{Tag: "email", Content: []wire.ChatdNode{{Tag: "do_verify", Content: []byte("true")}}}}}
	if result := EmailSetResultFromIQ(setting, "fixture@example.com"); result.Err != nil || result.Status != waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION {
		t.Fatal("binding requiring verification was treated as verified")
	}
	for _, wait := range []string{"0", "60", "-1", "invalid", ""} {
		t.Run(wait, func(t *testing.T) {
			response := wire.ChatdNode{Tag: "iq", Content: []wire.ChatdNode{{Tag: "verify_email", Content: []wire.ChatdNode{{Tag: "wait_time", Content: []byte(wait)}}}}}
			result := EmailOtpRequestResultFromIQ(response)
			valid := wait == "0" || wait == "60"
			if valid != (result.Err == nil) {
				t.Fatal("invalid verification cooldown was accepted")
			}
			if wait == "60" && result.WaitTime != time.Minute {
				t.Fatal("native resend cooldown was lost")
			}
		})
	}
}

func TestRejectedEmailSettingsPreserveUpstreamError(t *testing.T) {
	input := wacore.EngineAccountSettingsInput{Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY, Code: "001234"}
	result := AccountSettingsResultFromIQ(input, wire.ChatdNode{
		Tag: "iq", Attrs: map[string]string{"type": "error"},
		Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "405"}}},
	})
	err := shared.ToProtoError(result.Err)
	if result.Status != waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED || result.TwoFactorStatus != nil || err.GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED || !strings.Contains(err.GetMessage(), "405") {
		t.Fatal("upstream rejection was treated as a verified email")
	}
	if strings.Contains(err.GetMessage(), input.Code) {
		t.Fatal("email code leaked into the operation error")
	}
}

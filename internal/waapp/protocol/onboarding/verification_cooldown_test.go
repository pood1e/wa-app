package onboarding

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestVerificationCodeHonorsGlobalCooldown(t *testing.T) {
	for _, smsWait := range []int{0, 60, 600} {
		data := map[string]any{"sms_wait": smsWait, "retry_after": 300}
		got := VerificationCodeRetryAfter(data, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
		want := time.Duration(max(smsWait, 300)) * time.Second
		if got != want {
			t.Fatalf("retry delay = %v, want %v", got, want)
		}
	}
}

func TestVerificationCooldownErrorClassification(t *testing.T) {
	for _, data := range []map[string]any{
		{"status": "fail", "reason": "temporarily_unavailable"},
		{"status": "temporarily_unavailable"},
	} {
		err := shared.ToProtoError(WaProtocolError(data, "verification request is cooling down"))
		if err.GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED || !err.GetRetryable() {
			t.Fatal("cooldown was classified as a terminal registration rejection")
		}
	}
}

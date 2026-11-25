package onboarding

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

func TestRegistrationContinuationKeepsTheServerMethodOrder(t *testing.T) {
	for _, test := range []struct {
		name    string
		methods []any
		want    waappv1.VerificationDeliveryMethod
		invalid bool
	}{
		{"successor", []any{"wa_old", "sms", "email_otp"}, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, false},
		{"first when current absent", []any{"email_otp", "sms"}, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP, false},
		{"unsupported successor", []any{"wa_old", "future_method", "sms"}, 0, true},
		{"malformed successor", []any{"wa_old", 123, "sms"}, 0, true},
		{"last method", []any{"sms", "wa_old"}, 0, true},
		{"empty chain", nil, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{"status": "fail", "reason": "next_method_required", "fallback_methods": test.methods, "retry_after": 60, "sms_wait": 30}
			next, retry, err := registrationContinuation(data, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD)
			if (err != nil) != test.invalid {
				t.Fatal("continuation did not retain the immediate server-selected method")
			}
			if !test.invalid && (next.GetDeliveryMethod() != test.want || retry != time.Minute || next.GetAvailable()) {
				t.Fatal("continuation lost its required method or cooldown")
			}
		})
	}
}

func TestRegistrationContinuationRequiresFailureAndAvailableMethod(t *testing.T) {
	data := map[string]any{"status": "fail", "reason": "sms_required", "sms_wait": 30}
	next, retry, err := registrationContinuation(data, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD)
	if err != nil || next.GetDeliveryMethod() != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS || retry != 30*time.Second {
		t.Fatal("SMS continuation lost its method or wait", err)
	}
	data["status"] = "ok"
	if next, _, err := registrationContinuation(data, 0); next != nil || err != nil {
		t.Fatal("successful registration was treated as another verification step")
	}
	data["status"], data["sms_wait"] = "fail", -1
	if _, _, err := registrationContinuation(data, 0); err == nil {
		t.Fatal("continuation enabled an exhausted method")
	}
}

func TestRegistrationCertificateIsRedacted(t *testing.T) {
	data := map[string]any{"status": "fail", "reason": "next_method_required", "cert": "synthetic-certificate", "response_text": `{"cert":"synthetic-certificate"}`}
	redacted := SanitizeResponse(data)
	if redacted["cert"] != "<redacted>" || redacted["response_text"] != "<redacted>" || redacted["reason"] != "next_method_required" || data["cert"] != "synthetic-certificate" {
		t.Fatal("registration certificate leaked or response redaction changed protocol input")
	}
}

func TestAndroidContinuesRequiredVerificationWithTheSameIdentity(t *testing.T) {
	const old = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	const sms = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	var codes, registers int
	var identity url.Values
	flow := androidVerificationTestFlow(t, func(route string, query url.Values) (int, string) {
		if identity == nil {
			identity = query
		}
		for _, key := range []string{"authkey", "e_ident", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig", "fdid", "expid"} {
			if query.Get(key) != identity.Get(key) {
				t.Error("continuation replaced registration identity material")
			}
		}
		switch route {
		case "/v2/code":
			codes++
			return http.StatusOK, `{"status":"sent","length":6}`
		case "/v2/register":
			registers++
			if registers == 1 {
				return http.StatusOK, `{"status":"fail","reason":"next_method_required","fallback_methods":["wa_old","sms"]}`
			}
			if query.Get("method") != "sms" || query.Get("code") != "000007" {
				t.Error("continuation lost its selected method or fresh code")
			}
			return http.StatusOK, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`
		default:
			t.Error("unexpected continuation request")
			return http.StatusNotFound, ""
		}
	})
	if result := flow.RequestCode(t.Context(), old); result.Err != nil {
		t.Fatal("initial code request failed")
	}
	keys, _, err := flow.current.Export()
	if err != nil {
		t.Fatal(err)
	}
	result := flow.SubmitCode(t.Context(), old, "012345")
	if result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE || result.NextVerification.GetDeliveryMethod() != sms || result.OutcomeUnknown || codes != 1 || registers != 1 {
		t.Fatal("required next method was rejected, accepted as login, or automatically requested")
	}
	if result := flow.SubmitCode(t.Context(), old, "012345"); result.Err == nil || registers != 1 {
		t.Fatal("previous proof was reused after a required continuation")
	}
	if result := flow.RequestCode(t.Context(), old); result.Err == nil || codes != 1 {
		t.Fatal("continuation allowed a different method")
	}
	if result := flow.RequestCode(t.Context(), sms); result.Err != nil || codes != 2 {
		t.Fatal("explicit continuation request failed")
	}
	if result := flow.SubmitCode(t.Context(), sms, "000007"); result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED || registers != 2 {
		t.Fatal("fresh required verification did not complete registration")
	}
	material, err := flow.Material("12025550123", "synthetic-lid")
	if err != nil || !proto.Equal(keys, material.GetCredentials()) || flow.requiredVerification != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		t.Fatal("continuation changed credentials or retained a completed restriction", err)
	}
}

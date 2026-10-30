package wire

import (
	"bytes"
	"log"
	"net/url"
	"strings"
	"testing"
)

func TestRegistrationResponseDiagnosticsCorrelateAndRedact(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	endpoint, err := url.Parse(DefaultWARegisterURL)
	if err != nil {
		t.Fatal(err)
	}
	const enc = "PRIVATE_ENC_SENTINEL"
	LogWASafeEnvelopeShape(endpoint, WaSafeEnvelope{Enc: enc}, "synthetic")
	logRegistrationResponse(endpoint, 200, map[string]any{
		"status": "fail", "reason": "missing", "param": "code", "method": "wa_old", "length": float64(6), "retry_after": float64(120),
		"auth_response": "PRIVATE_AUTH_SENTINEL", "context": "PRIVATE_CONTEXT_SENTINEL", "login": "PRIVATE_PHONE_SENTINEL", "code": "PRIVATE_OTP_SENTINEL",
	}, enc)
	for _, want := range []string{"kind=register", "status_code=200", "status=fail", "reason=missing", "param=code", "method=wa_old", "retry_after=120", "length=6"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("diagnostic omitted %s", want)
		}
	}
	if strings.Count(output.String(), "enc_sha256="+EncHash(enc)) != 2 {
		t.Fatal("request and response diagnostics cannot be correlated")
	}
	for _, field := range []string{"status", "reason", "param", "method", "error_code", "retry_after", "length"} {
		logRegistrationResponse(endpoint, 200, map[string]any{field: "PRIVATE_UNSTRUCTURED_SENTINEL\nstatus=sent"}, enc)
	}
	if strings.Contains(output.String(), "PRIVATE_") || strings.Contains(output.String(), "status=sent") {
		t.Fatal("diagnostics leaked request material or an unrecognized response value")
	}
}

func TestRegistrationJSONDiagnosticsSupportDirectEndpoints(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	endpoint, err := url.Parse("https://v.whatsapp.net/v2/code")
	if err != nil {
		t.Fatal(err)
	}
	LogRegistrationResponse(endpoint, 200, []byte(`{"status":"fail","reason":"blocked","method":"sms","login":"PRIVATE_LOGIN_SENTINEL","token":"PRIVATE_TOKEN_SENTINEL"}`), "")
	for _, want := range []string{"kind=code", "status=fail", "reason=blocked", "method=sms", "enc_sha256=absent"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("diagnostic omitted %s", want)
		}
	}
	LogRegistrationResponse(endpoint, 502, nil, "")
	LogRegistrationResponse(endpoint, 200, []byte(`{"status":"PRIVATE_STATUS_SENTINEL"`), "")
	if !strings.Contains(output.String(), "status_code=502") || strings.Contains(output.String(), "PRIVATE_") {
		t.Fatal("JSON diagnostics lost HTTP failure status or leaked response content")
	}
}

func TestRegistrationErrorCodeExcludesUnstructuredResponseValues(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{float64(101), "101"},
		{"-2", "-2"},
		{"PRIVATE_TOKEN_SENTINEL", "invalid"},
		{"447700900123", "invalid"},
		{float64(1.5), "invalid"},
		{map[string]any{"token": "PRIVATE_TOKEN_SENTINEL"}, "invalid"},
	} {
		if got := registrationErrorCode(map[string]any{"error_code": tc.value}); got != tc.want {
			t.Errorf("error code classification = %q, want %q", got, tc.want)
		}
	}
	if got := registrationErrorCode(nil); got != "absent" {
		t.Fatalf("missing error code = %q", got)
	}
}

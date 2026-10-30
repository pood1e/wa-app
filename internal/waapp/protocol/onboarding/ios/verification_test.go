package ios

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

const oldDeviceMethod = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD

func TestFlowVerificationAvailability(t *testing.T) {
	for _, test := range []struct {
		name, response string
		method         waappv1.VerificationDeliveryMethod
		available      bool
		seconds        int64
	}{
		{"available SMS", `{"sms_wait":0}`, smsMethod, true, 0},
		{"cooling SMS", `{"sms_wait":60}`, smsMethod, false, 60},
		{"unavailable SMS", `{"sms_wait":-1,"retry_after":60}`, smsMethod, false, 0},
		{"retry after", `{"retry_after":300}`, smsMethod, false, 300},
		{"long cooldown", `{"sms_wait":7200}`, smsMethod, false, 7200},
		{"oversized cooldown", `{"sms_wait":"4611686018427387904"}`, smsMethod, false, int64((1<<63 - 1) / time.Second)},
		{"available old device", `{"wa_old_eligible":1,"wa_old_wait":"0"}`, oldDeviceMethod, true, 0},
		{"SMS cooldown is independent", `{"sms_wait":300,"wa_old_eligible":1,"wa_old_wait":0}`, oldDeviceMethod, true, 0},
		{"old device cooldown", `{"wa_old_eligible":1,"wa_old_wait":300}`, oldDeviceMethod, false, 300},
		{"missing eligibility", `{"wa_old_wait":0}`, oldDeviceMethod, false, 0},
		{"missing old-device wait", `{"wa_old_eligible":1}`, oldDeviceMethod, false, 0},
		{"ineligible old device", `{"wa_old_eligible":0,"wa_old_wait":0}`, oldDeviceMethod, false, 0},
		{"unsupported eligibility", `{"wa_old_eligible":4,"wa_old_wait":0}`, oldDeviceMethod, false, 0},
		{"exhausted old device", `{"wa_old_eligible":1,"wa_old_wait":-1,"retry_after":300}`, oldDeviceMethod, false, 0},
		{"global cooldown", `{"wa_old_eligible":1,"wa_old_wait":0,"retry_after":300}`, oldDeviceMethod, false, 300},
		{"available email", `{"email_otp_eligible":1,"email_otp_wait":0}`, emailMethod, true, 0},
		{"email independent of SMS", `{"sms_wait":300,"email_otp_eligible":1,"email_otp_wait":0}`, emailMethod, true, 0},
		{"email cooldown", `{"email_otp_eligible":1,"email_otp_wait":60}`, emailMethod, false, 60},
		{"missing email eligibility", `{"email_otp_wait":0}`, emailMethod, false, 0},
		{"missing email wait", `{"email_otp_eligible":1}`, emailMethod, false, 0},
		{"unavailable email", `{"email_otp_eligible":0,"email_otp_wait":0}`, emailMethod, false, 0},
		{"exhausted email", `{"email_otp_eligible":1,"email_otp_wait":-1}`, emailMethod, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"status":"fail","reason":"incorrect",` + test.response[1:]
			client := &http.Client{Transport: registrationTransport(func(*http.Request) (*http.Response, error) { return jsonResponse(body), nil })}
			flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), client, Dependencies{})
			if err != nil {
				t.Fatal(err)
			}
			defer flow.Close(t.Context())
			if _, err := flow.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, status := range flow.VerificationMethods() {
				if status.GetDeliveryMethod() == test.method {
					seconds := int64(status.GetCooldown().AsDuration() / time.Second)
					if status.GetAvailable() != test.available || seconds != test.seconds {
						t.Fatalf("got available=%t cooldown=%d, want available=%t cooldown=%d", status.GetAvailable(), seconds, test.available, test.seconds)
					}
					return
				}
			}
			t.Fatal("verification channel missing")
		})
	}
}

func TestOldDeviceTransferRetainsKeysAfterUncertainRegistration(t *testing.T) {
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan url.Values, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost || r.TLS == nil || r.PostForm.Has("H") || r.Header.Get("Authorization") != "" {
			t.Error("worker-free transfer changed the unsigned iOS envelope")
		}
		frame, err := base64.URLEncoding.DecodeString(r.PostForm.Get("ENC"))
		if err != nil {
			t.Error(err)
			return
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
		if err != nil {
			t.Error(err)
			return
		}
		requests <- query
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":-1,"wa_old_eligible":1,"wa_old_wait":0,"wa_old_length":8}`)
		case "/code":
			if query.Get("method") != "wa_old" {
				t.Error("old-device verification used another channel")
			}
			_, _ = io.WriteString(w, `{"status":"sent","method":"wa_old","sms_length":6,"wa_old_length":8}`)
		case "/register":
			if query.Get("method") != "wa_old" || query.Get("code") != "12345678" || query.Get("entered") != "1" {
				t.Error("registration lost its selected method or code")
			}
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Error("unexpected iOS transfer route")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), server.Client(), Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close(context.Background())
	flow.http.baseURL, flow.http.serverKey = server.URL+"/", serverKey.PublicKey().Bytes()
	if _, err := flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.RequestCode(t.Context(), smsMethod); !errors.Is(err, ErrCodeCooldown) {
		t.Fatal("unavailable SMS request reached the network")
	}
	if response, err := flow.RequestCode(t.Context(), oldDeviceMethod); err != nil || response.GetMethod() != "wa_old" {
		t.Fatal("old-device code request failed", err)
	}
	login := syntheticPhone().GetCountryCallingCode() + syntheticPhone().GetNationalNumber()
	if _, err := flow.Material(login, login); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("unsubmitted flow exposed recoverable material")
	}
	if _, err := flow.SubmitCode(t.Context(), smsMethod, "12345678"); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("submission changed the verification channel")
	}
	if _, err := flow.SubmitCode(t.Context(), oldDeviceMethod, "123456"); err == nil {
		t.Fatal("submission ignored the selected channel's code length")
	}
	_, err = flow.SubmitCode(t.Context(), oldDeviceMethod, "12345678")
	var upstream *HTTPStatusError
	if !errors.As(err, &upstream) || upstream.StatusCode != 502 || !flow.OutcomeUnknown() {
		t.Fatal("registration lost its unknown outcome")
	}
	if len(requests) != 3 {
		t.Fatal("transfer replayed an HTTP request")
	}
	first := <-requests
	for range 2 {
		for key, value := range <-requests {
			if first.Has(key) && key != "t" && first.Get(key) != value[0] {
				t.Fatalf("transfer changed per-flow %s", key)
			}
		}
	}
	material, err := flow.Material(login, login)
	if err != nil || !bytes.Equal(material.GetCredentials().GetChatStatic().GetPrivate(), flow.keys.GetNoisePrivateKey()) {
		t.Fatal("uncertain transfer lost its original login keys")
	}
	if _, err := flow.Material(login, "another-account"); err == nil {
		t.Fatal("uncertain transfer accepted a different account")
	}
	if _, err := flow.KeyMaterial(); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("uncertain transfer reported confirmed key material")
	}
	retainedKey := bytes.Clone(material.GetCredentials().GetChatStatic().GetPrivate())
	if err := flow.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(material.GetCredentials().GetChatStatic().GetPrivate(), retainedKey) {
		t.Fatal("closing transfer destroyed independent recovery material")
	}
}

package onboarding

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestIOSPasswordFetchesPublicKeyInsideFirstTimeAddingFlow(t *testing.T) {
	key, _ := protocoltest.PasswordKey(t)
	var codes, registrations, keyReads int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/graphql":
			keyReads++
			var input struct {
				Token string `json:"access_token"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.Token == "" || r.Header.Get("Authorization") != "" {
				t.Error("new account public-key lookup required saved authorization")
			}
			_ = json.NewEncoder(w).Encode(struct {
				Data any `json:"data"`
			}{struct {
				Key *protocolv1.PasswordEncryptionKey `json:"wa_password_encryption_key"`
			}{key}})
		case "/v2/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0}`)
		case "/v2/code":
			codes++
			_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6}`)
		case "/v2/register":
			registrations++
			if registrations == 1 {
				_, _ = io.WriteString(w, `{"status":"fail","reason":"security_code"}`)
			} else {
				_, _ = io.WriteString(w, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`)
			}
		default:
			t.Error("unexpected registration request")
		}
	}))
	t.Cleanup(server.Close)
	profile, err := ios.DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	flow, err := NewIOSFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, profile, "", ios.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	transport := flow.http.Transport.(*http.Transport)
	configuration := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&tls.Dialer{Config: configuration}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	const password = " Password1 "
	if flow.SubmitPassword(t.Context(), password).Err == nil || keyReads != 0 {
		t.Fatal("password lookup started before first-factor verification")
	}
	if flow.Probe(t.Context()).Err != nil || flow.RequestCode(t.Context(), waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS).Err != nil {
		t.Fatal("iOS first-factor preparation failed")
	}
	result := flow.SubmitCode(t.Context(), waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "123456")
	if result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD {
		t.Fatal("first-time account did not preserve its password challenge")
	}
	result = flow.SubmitPassword(t.Context(), password)
	if result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED || keyReads != 1 || codes != 1 || registrations != 2 {
		t.Fatal("iOS password continuation failed or repeated first-factor verification")
	}
}

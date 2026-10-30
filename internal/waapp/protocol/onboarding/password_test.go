package onboarding

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestAndroidPasswordUsesFirstFactorKeysAndCanonicalEnvelope(t *testing.T) {
	key, private := protocoltest.PasswordKey(t)
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const password = " Password1 "
	var first url.Values
	var codes, registrations, passwords, keyReads int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			keyReads++
			_ = json.NewEncoder(w).Encode(struct {
				Data any `json:"data"`
			}{struct {
				Key *protocolv1.PasswordEncryptionKey `json:"wa_password_encryption_key"`
			}{key}})
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		frame, err := base64.RawURLEncoding.DecodeString(r.PostForm.Get("ENC"))
		if err != nil {
			t.Error(err)
			return
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
		if err != nil {
			t.Error(err)
			return
		}
		route := r.URL.Query().Get("_")
		w.Header().Set("Content-Type", "application/json")
		if route == "/v2/reg_onboard_abprop" {
			_, _ = io.WriteString(w, `{"status":"ok","retry_after":3600,"exp_cfg":"[{\"config_code\":3753,\"config_value\":0}]"}`)
			return
		}
		if first == nil {
			first = query
		}
		for _, name := range []string{"authkey", "e_ident", "e_skey_val", "id", "fdid", "expid", "backup_token", "access_session_id"} {
			if first.Get(name) == "" || first.Get(name) != query.Get(name) {
				t.Errorf("password verification replaced %s", name)
			}
		}
		switch route {
		case "/v2/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0}`)
		case "/v2/code":
			codes++
			_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6,"auth_response":"PRIVATE_FIRST_FACTOR_RESPONSE"}`)
		case "/v2/register":
			if query.Get("method") != "password" {
				registrations++
				_, _ = io.WriteString(w, `{"status":"fail","reason":"security_code"}`)
				return
			}
			passwords++
			if query.Has("type") || query.Has("auth_response") || query.Get("context") != "twofac_dynamic" {
				t.Error("password request differs from KotlinRegistrationBridge.A0p/A0R")
			}
			text, err := protocoltest.OpenPassword(private, query.Get("code"))
			if err != nil || text != password {
				t.Error("password was not sealed without changing its text")
			}
			if strings.Contains(string(frame), password) {
				t.Error("password escaped its encrypted envelopes")
			}
			_, _ = io.WriteString(w, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`)
		default:
			t.Error("unexpected password registration route")
		}
	}))
	t.Cleanup(server.Close)
	build := protocoltest.AndroidBuild(t)
	build.ServerPublicKey = serverKey.PublicKey().Bytes()
	flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, build, protocoltest.AndroidClient(), "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	configuration := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	flow.http.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&tls.Dialer{Config: configuration}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	flow.http.Client.Transport.(*http.Transport).DialTLSContext = flow.http.DialTLSContext
	if flow.SubmitPassword(t.Context(), password).Err == nil || keyReads != 0 {
		t.Fatal("password was sent before first-factor verification")
	}
	if flow.Probe(t.Context()).Err != nil || flow.RequestCode(t.Context(), waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS).Err != nil {
		t.Fatal("first-factor preparation failed")
	}
	result := flow.SubmitCode(t.Context(), waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "123456")
	if result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD || result.PasswordChallenge == nil {
		t.Fatal("security_code was treated as registration rejection")
	}
	result = flow.SubmitPassword(t.Context(), password)
	if result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED || codes != 1 || registrations != 1 || passwords != 1 || keyReads != 1 {
		t.Fatal("password verification repeated a first-factor operation or did not complete")
	}
}

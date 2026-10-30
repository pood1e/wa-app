package onboarding

import (
	"bytes"
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
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestAndroidVerificationPreservesPhoneTypeAndResponseMaterial(t *testing.T) {
	for _, method := range []waappv1.VerificationDeliveryMethod{
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD,
	} {
		for _, withMaterial := range []bool{false, true} {
			name := RegistrationMethodName(method, "") + "/without_response_material"
			if withMaterial {
				name = RegistrationMethodName(method, "") + "/with_response_material"
			}
			t.Run(name, func(t *testing.T) {
				serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				requests := make(chan url.Values, 4)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					frame, err := base64.RawURLEncoding.DecodeString(r.PostForm.Get("ENC"))
					if err != nil {
						t.Error("decode registration envelope")
						return
					}
					query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
					if err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					route := r.URL.Query().Get("_")
					if route == "/v2/reg_onboard_abprop" {
						_, _ = io.WriteString(w, `{"status":"ok","retry_after":3600,"exp_cfg":"[{\"config_code\":3753,\"config_value\":0}]"}`)
						return
					}
					requests <- query
					if query.Get("type") != "0" {
						t.Errorf("%s omitted the native PHONE contact-point type", route)
					}
					switch route {
					case "/v2/exist":
						_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0,"wa_old_wait":0,"wa_old_eligible":1}`)
					case "/v2/code":
						if query.Get("method") != RegistrationMethodName(method, "") {
							t.Error("code request changed its verification channel")
						}
						response := map[string]any{"status": "sent", "method": RegistrationMethodName(method, ""), "length": 6}
						if withMaterial {
							response["type"] = "1"
							for key, value := range syntheticVerificationMaterial() {
								response[key] = value
							}
						}
						if err := json.NewEncoder(w).Encode(response); err != nil {
							t.Error(err)
						}
					case "/v2/register":
						if query.Get("method") != RegistrationMethodName(method, "") || query.Get("code") != "123456" {
							t.Error("registration changed the requested channel or code")
						}
						for key, value := range syntheticVerificationMaterial() {
							if withMaterial && query.Get(key) != value {
								t.Errorf("registration did not preserve response field %s", key)
							} else if !withMaterial && query.Has(key) {
								t.Errorf("registration invented response field %s", key)
							}
						}
						_, _ = io.WriteString(w, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`)
					default:
						t.Error("unexpected registration route")
						w.WriteHeader(http.StatusNotFound)
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
				if result := flow.Probe(t.Context()); result.Err != nil {
					t.Fatal("phone verification probe failed")
				}
				if result := flow.RequestCode(t.Context(), method); result.Err != nil {
					t.Fatal("phone verification code request failed")
				}
				if result := flow.SubmitCode(t.Context(), method, "123456"); result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
					t.Fatal("phone verification registration failed")
				}
				if len(requests) != 3 {
					t.Fatalf("verification requests = %d, want 3", len(requests))
				}
				first := <-requests
				for range 2 {
					next := <-requests
					for _, key := range []string{"authkey", "e_ident", "e_skey_val", "id", "fdid", "backup_token"} {
						if first.Get(key) == "" || first.Get(key) != next.Get(key) {
							t.Errorf("verification changed per-flow %s", key)
						}
					}
				}
				material, err := flow.Material("12025550123", "synthetic-lid")
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := protojson.Marshal(material)
				if err != nil || bytes.Contains(encoded, []byte("PRIVATE_RESPONSE_SENTINEL")) {
					t.Fatal("temporary verification response material escaped the adding flow")
				}
			})
		}
	}
}

func syntheticVerificationMaterial() map[string]string {
	return map[string]string{
		"login": "synthetic-response-login", "auth_response": "PRIVATE_RESPONSE_SENTINEL+/%=",
		"context": "synthetic-response-context",
	}
}

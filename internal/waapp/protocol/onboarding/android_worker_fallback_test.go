package onboarding

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type unavailableAndroidSDK struct {
	waappv1.AndroidAttestationServiceClient
	calls atomic.Int32
}

func (c *unavailableAndroidSDK) CreateContext(context.Context, *waappv1.CreateAndroidAttestationContextRequest, ...grpc.CallOption) (*waappv1.AndroidKeyAttestation, error) {
	c.calls.Add(1)
	return nil, status.Error(codes.Unavailable, "synthetic worker outage")
}

func TestAndroidWorkerFallbackCompletesHostOwnedRequestsOverTLS(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "unconfigured"
		if configured {
			name = "offline"
		}
		t.Run(name, func(t *testing.T) {
			serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			requests := make(chan url.Values, 8)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				route := r.URL.Query().Get("_")
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodPost || r.TLS == nil || r.Header.Get("Authorization") != "" || r.PostForm.Get("H") != "" ||
					r.PostForm.Has("H") != (route != "/v2/reg_onboard_abprop") {
					t.Error("worker outage changed the native unsigned envelope")
				}
				frame, err := base64.RawURLEncoding.DecodeString(r.PostForm.Get("ENC"))
				if err != nil {
					t.Error("invalid registration envelope")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if route == "/v2/reg_onboard_abprop" {
					_, _ = io.WriteString(w, `{"status":"ok","retry_after":3600,"exp_cfg":"[{\"config_code\":3753,\"config_value\":1}]"}`)
					return
				}
				requests <- query
				assertAndroidUnavailableIntegrity(t, query)
				switch route {
				case "/v2/exist":
					_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0}`)
				case "/v2/code":
					_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6}`)
				case "/v2/register":
					if query.Get("code") != "123456" {
						t.Error("OTP did not reach the encrypted request")
					}
					_, _ = io.WriteString(w, `{"status":"ok","login":"447700900000","lid":"synthetic-lid"}`)
				default:
					t.Error("unexpected registration route")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			build := protocoltest.AndroidBuild(t)
			build.ServerPublicKey = serverKey.PublicKey().Bytes()
			var dependencies AndroidDependencies
			var wantSDKCalls int32
			sdk := new(unavailableAndroidSDK)
			if configured {
				dependencies.Attestation = sdk
				wantSDKCalls = 1
			}
			flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "44", NationalNumber: "7700900000"}, build, protocoltest.AndroidClient(), "", dependencies)
			if err != nil {
				t.Fatal(err)
			}
			defer flow.Close()
			configuration := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			flow.http.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&tls.Dialer{Config: configuration}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			if result := flow.Probe(t.Context()); result.Err != nil {
				t.Fatal("unsigned probe failed")
			}
			method := waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
			if result := flow.RequestCode(t.Context(), method); result.Err != nil || result.Status != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT {
				t.Fatal("unsigned code request failed")
			}
			if result := flow.SubmitCode(t.Context(), method, "123456"); result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
				t.Fatal("unsigned registration failed")
			}
			if len(requests) != 3 || sdk.calls.Load() != wantSDKCalls {
				t.Fatal("flow repeated registration requests or retried the unavailable worker")
			}
			first := <-requests
			for range 2 {
				next := <-requests
				for _, key := range []string{"authkey", "e_ident", "e_skey_val", "e_skey_sig", "fdid", "expid", "id"} {
					if first.Get(key) == "" || first.Get(key) != next.Get(key) {
						t.Fatalf("fallback changed per-flow %s", key)
					}
				}
			}
			if material, err := flow.Material("447700900000", "synthetic-lid"); err != nil || material.GetCredentials() == nil || material.GetRecovery() == nil {
				t.Fatal("completed fallback lost its account material")
			}
		})
	}
}

func assertAndroidUnavailableIntegrity(t *testing.T, query url.Values) {
	t.Helper()
	public, err := base64.RawURLEncoding.DecodeString(query.Get("authkey"))
	if err != nil || len(public) != 32 {
		t.Error("missing flow Noise public key")
		return
	}
	key := sha256.Sum256([]byte(base64.StdEncoding.EncodeToString(public)))
	for _, name := range []string{"gpia", "_gg"} {
		encrypted, err := base64.StdEncoding.DecodeString(query.Get(name))
		if err != nil || len(encrypted) < 32 {
			t.Errorf("missing native failure envelope %s", name)
			continue
		}
		plain, err := wire.AesCBCPKCS7Decrypt(encrypted[16:], key[:], encrypted[:16])
		if err != nil {
			t.Error(err)
			continue
		}
		var outcome struct {
			Code         int32  `json:"code"`
			CompactCode  int32  `json:"_ic"`
			Token        string `json:"token"`
			CompactToken string `json:"_it"`
		}
		if err := json.Unmarshal(plain, &outcome); err != nil || max(outcome.Code, outcome.CompactCode) != 1000 || outcome.Token != "" || outcome.CompactToken != "" {
			t.Errorf("%s did not preserve the native unavailable outcome", name)
		}
	}
}

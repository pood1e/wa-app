package ios

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestWorkerFallbackCompletesHostOwnedRequestsOverTLS(t *testing.T) {
	for _, stage := range []string{"unconfigured", "create", "key proof", "assertion", "lost context"} {
		t.Run(stage, func(t *testing.T) {
			var keyProofs, assertions, releases atomic.Int32
			var signer waappv1.AppAttestServiceClient
			offline := status.Error(codes.Unavailable, "synthetic worker outage")
			if stage != "unconfigured" {
				worker := &attestServer{
					bind: syntheticAttestContext,
					attest: func(*waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
						keyProofs.Add(1)
						if stage == "key proof" {
							return nil, offline
						}
						return syntheticAttestBinding(), nil
					},
					sign: func(_ context.Context, request *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
						attempt := assertions.Add(1)
						if stage == "lost context" {
							if attempt == 1 {
								return &waappv1.GenerateAppAttestAssertionResponse{Context: request.Context, Assertion: request.ClientDataHash}, nil
							}
							return nil, status.Error(codes.NotFound, "synthetic worker restart")
						}
						return nil, offline
					},
					release: func(*waappv1.AppAttestContext) { releases.Add(1) }, releaseErr: offline,
				}
				if stage == "create" {
					worker.createErr = offline
				}
				signer = attestClient(t, worker)
			}
			serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			requests := make(chan url.Values, 8)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				wantAssertion := stage == "lost context" && r.URL.Path == "/exist"
				wantAuthorization := stage == "assertion" || stage == "lost context"
				if r.Method != http.MethodPost || r.TLS == nil || r.PostForm.Has("H") != wantAssertion ||
					(r.Header.Get("Authorization") != "") != wantAuthorization {
					t.Error("worker outage changed the native unsigned envelope")
				}
				if wantAssertion {
					if _, err := openRegistrationRequest(r, serverKey); err != nil {
						t.Error("healthy request lost its frame proof")
					}
				}
				frame, err := base64.URLEncoding.DecodeString(r.PostForm.Get("ENC"))
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
				requests <- query
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/exist":
					_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0}`)
				case "/code":
					_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6}`)
				case "/register":
					if query.Get("code") != "123456" {
						t.Error("OTP did not reach the encrypted request")
					}
					_, _ = io.WriteString(w, `{"status":"ok","login":"synthetic-login","lid":"synthetic-lid"}`)
				default:
					t.Error("unexpected registration route")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), server.Client(), Dependencies{Attestation: signer})
			if err != nil {
				t.Fatal(err)
			}
			defer flow.Close(t.Context())
			flow.http.baseURL, flow.http.serverKey = server.URL+"/", serverKey.PublicKey().Bytes()
			if result, err := flow.Probe(t.Context()); err != nil || result.GetReason() != "incorrect" {
				t.Fatal("unsigned probe failed")
			}
			if result, err := flow.RequestCode(t.Context(), smsMethod); err != nil || result.GetStatus() != "sent" {
				t.Fatal("unsigned code request failed")
			}
			if result, err := flow.SubmitCode(t.Context(), smsMethod, "123456"); err != nil || result.GetStatus() != "ok" {
				t.Fatal("unsigned registration failed")
			}
			if len(requests) != 3 {
				t.Fatal("fallback repeated registration HTTP requests")
			}
			first := <-requests
			for range 2 {
				next := <-requests
				for _, key := range []string{"authkey", "e_ident", "e_skey_val", "e_skey_sig", "fdid", "expid", "id", "ios_id"} {
					if first.Get(key) == "" || first.Get(key) != next.Get(key) {
						t.Fatalf("fallback changed per-flow %s", key)
					}
				}
			}
			if _, err := flow.KeyMaterial(); err != nil {
				t.Fatal("completed fallback lost its account material")
			}
			if err := flow.Close(t.Context()); err != nil {
				t.Fatal("unavailable release blocked delivery")
			}
			if stage == "unconfigured" || stage == "create" {
				if keyProofs.Load() != 0 || assertions.Load() != 0 || releases.Load() != 0 {
					t.Fatal("missing context reached worker operations")
				}
			} else {
				var wantAssertions int32
				if stage == "assertion" {
					wantAssertions = 1
				} else if stage == "lost context" {
					wantAssertions = 2
				}
				if keyProofs.Load() != 1 || releases.Load() != 1 || assertions.Load() != wantAssertions {
					t.Fatal("degraded flow kept calling worker operations")
				}
			}
		})
	}
}

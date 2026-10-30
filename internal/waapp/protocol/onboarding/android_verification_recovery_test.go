package onboarding

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestAndroidUnconfirmedCodeDeliveryCannotRegister(t *testing.T) {
	for _, response := range []string{
		``, `{}`, `<html>upstream unavailable</html>`,
		`{"reason":"temporarily_unavailable","retry_after":60}`,
		`{"status":"fail","reason":"temporarily_unavailable","retry_after":60}`,
		`{"status":"sent","method":"sms","length":6}`,
	} {
		t.Run(response, func(t *testing.T) {
			var registers atomic.Int32
			flow := androidVerificationTestFlow(t, func(route string, _ url.Values) (int, string) {
				if route == "/v2/register" {
					registers.Add(1)
				}
				return http.StatusOK, response
			})
			method := waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
			result := flow.RequestCode(t.Context(), method)
			if result.Err == nil || result.Status != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED {
				t.Fatal("unconfirmed or mismatched delivery was accepted")
			}
			if flow.verification != nil {
				t.Fatal("unconfirmed delivery retained usable verification context")
			}
			if got := flow.SubmitCode(t.Context(), method, "012345"); got.Err == nil || registers.Load() != 0 {
				t.Fatal("registration was sent without confirmed delivery through the selected channel")
			}
		})
	}
}

func TestAndroidMissingOrStaleVerificationCanRequestFreshCode(t *testing.T) {
	for _, method := range []waappv1.VerificationDeliveryMethod{
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD,
	} {
		for _, reason := range []string{"missing", "stale"} {
			t.Run(RegistrationMethodName(method, "")+"/"+reason, func(t *testing.T) {
				var codes, registers atomic.Int32
				requests := make(chan url.Values, 5)
				flow := androidVerificationTestFlow(t, func(route string, query url.Values) (int, string) {
					requests <- query
					switch route {
					case "/v2/exist":
						return http.StatusOK, `{"status":"fail","reason":"incorrect"}`
					case "/v2/code":
						attempt := codes.Add(1)
						if attempt == 2 && query.Has("login") {
							t.Error("fresh request reused rejected verification response material")
						}
						return http.StatusOK, fmt.Sprintf(`{"status":"sent","method":%q,"length":6,"auth_response":"SYNTHETIC_RESPONSE_%d+/=","login":"synthetic-login"}`, RegistrationMethodName(method, ""), attempt)
					case "/v2/register":
						attempt := registers.Add(1)
						if query.Get("method") != RegistrationMethodName(method, "") || query.Get("auth_response") != fmt.Sprintf("SYNTHETIC_RESPONSE_%d+/=", attempt) {
							t.Error("registration changed the channel or used stale response material")
						}
						if attempt == 1 {
							if query.Get("code") != "012345" {
								t.Error("initial submission lost its leading zero")
							}
							return http.StatusOK, fmt.Sprintf(`{"status":"fail","reason":%q,"retry_after":120}`, reason)
						}
						if query.Get("code") != "000007" {
							t.Error("fresh submission lost its leading zeros")
						}
						return http.StatusOK, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`
					default:
						t.Error("unexpected verification route")
						return http.StatusNotFound, ""
					}
				})
				if result := flow.Probe(t.Context()); result.Err != nil {
					t.Fatal("probe failed")
				}
				if result := flow.RequestCode(t.Context(), method); result.Err != nil {
					t.Fatal("initial code request failed")
				}
				result := flow.SubmitCode(t.Context(), method, "012345")
				if result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE || result.OutcomeUnknown || result.RetryAfter != 120*time.Second || shared.ToProtoError(result.Err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED {
					t.Fatal("missing or stale response did not require fresh verification with its cooldown")
				}
				if codes.Load() != 1 || registers.Load() != 1 || flow.verification != nil {
					t.Fatal("rejection retried automatically or retained stale response material")
				}
				if repeat := flow.SubmitCode(t.Context(), method, "012345"); repeat.Err == nil || registers.Load() != 1 {
					t.Fatal("rejected verification was submitted again before requesting a fresh code")
				}
				if fresh := flow.RequestCode(t.Context(), method); fresh.Err != nil {
					t.Fatal("fresh code request failed")
				}
				if fresh := flow.SubmitCode(t.Context(), method, "000007"); fresh.Err != nil || fresh.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
					t.Fatal("fresh verification did not complete")
				}
				if len(requests) != 5 {
					t.Fatalf("verification requests = %d, want 5", len(requests))
				}
				first := <-requests
				for range 4 {
					next := <-requests
					for _, key := range []string{"authkey", "e_ident", "e_skey_val", "id", "fdid", "backup_token"} {
						if first.Get(key) == "" || first.Get(key) != next.Get(key) {
							t.Errorf("verification recovery replaced per-flow %s", key)
						}
					}
				}
			})
		}
	}
}

func TestAndroidOtherRegistrationOutcomesRetainConfirmedVerification(t *testing.T) {
	for _, tc := range []struct {
		response string
		unknown  bool
	}{
		{"", true}, {"{}", true}, {"<html>upstream unavailable</html>", true},
		{`{"status":"fail","reason":"incorrect"}`, false},
		{`{"status":"fail","reason":"missing_param","param":"type"}`, false},
	} {
		t.Run(tc.response, func(t *testing.T) {
			var registers atomic.Int32
			flow := androidVerificationTestFlow(t, func(route string, _ url.Values) (int, string) {
				if route == "/v2/code" {
					return http.StatusOK, `{"status":"sent","method":"wa_old","length":6}`
				}
				if registers.Add(1) == 1 {
					return http.StatusOK, tc.response
				}
				return http.StatusOK, `{"status":"ok","login":"12025550123","lid":"synthetic-lid"}`
			})
			method := waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
			if result := flow.RequestCode(t.Context(), method); result.Err != nil {
				t.Fatal("code request failed")
			}
			result := flow.SubmitCode(t.Context(), method, "012345")
			if result.Err == nil || result.OutcomeUnknown != tc.unknown || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED || flow.verification == nil {
				t.Fatal("uncertain outcome or another rejection was treated as missing verification")
			}
			if result := flow.SubmitCode(t.Context(), method, "000007"); result.Err != nil || result.Status != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
				t.Fatal("explicit submission could not reuse the confirmed verification context")
			}
		})
	}
}

func androidVerificationTestFlow(t *testing.T, respond func(string, url.Values) (int, string)) *AndroidFlow {
	t.Helper()
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("_") == "/v2/reg_onboard_abprop" {
			_, _ = io.WriteString(w, `{"status":"ok","retry_after":3600,"exp_cfg":"[{\"config_code\":3753,\"config_value\":0}]"}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error("parse encrypted registration form")
			return
		}
		frame, err := base64.RawURLEncoding.DecodeString(r.PostForm.Get("ENC"))
		if err != nil {
			t.Error("decode registration envelope")
			return
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
		if err != nil {
			t.Error("open registration envelope")
			return
		}
		code, response := respond(r.URL.Query().Get("_"), query)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, response)
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
	return flow
}

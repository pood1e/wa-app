package ios

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type recaptchaClient func(context.Context, *waappv1.RecaptchaLoginTokenRequest) (*waappv1.RecaptchaLoginToken, error)

func (c recaptchaClient) GenerateLoginToken(ctx context.Context, request *waappv1.RecaptchaLoginTokenRequest, _ ...grpc.CallOption) (*waappv1.RecaptchaLoginToken, error) {
	return c(ctx, request)
}

func TestCodeChallengeRetainsKeysAndUsesFreshSDKProofs(t *testing.T) {
	for _, tc := range []struct {
		name          string
		next          string
		providerError error
		maxChallenges int
		proofs        int
		requests      int
		wantStatus    string
		wantError     bool
	}{
		{name: "accepted", next: "recaptcha", maxChallenges: 1, proofs: 1, requests: 2, wantStatus: "sent"},
		{name: "bounded rechallenge", next: "recaptcha", maxChallenges: 4, proofs: 2, requests: 3, wantStatus: "fail"},
		{name: "unrelated challenge", next: "email", maxChallenges: 1, requests: 1, wantStatus: "fail"},
		{name: "SDK unavailable", next: "recaptcha", providerError: status.Error(codes.Unavailable, "synthetic"), maxChallenges: 1, proofs: 1, requests: 1, wantStatus: "fail", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			const token = "synthetic+token /?%=&"
			var codeRequests, sdkCalls int
			var original url.Values
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				query, openErr := openRegistrationRequest(request, key)
				if openErr != nil {
					t.Error(openErr)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.URL.Path == "/exist" {
					original = query
					_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0}`)
					return
				}
				codeRequests++
				for _, field := range []string{"authkey", "e_ident", "e_skey_val", "fdid", "ios_id"} {
					if original.Get(field) == "" || query.Get(field) != original.Get(field) {
						t.Errorf("challenge changed retained registration field %s", field)
					}
				}
				if codeRequests > 1 {
					var payload struct {
						Token       string `json:"token"`
						TokenLength int    `json:"token_length"`
						TokenAge    *int   `json:"token_age"`
					}
					if err := json.Unmarshal([]byte(query.Get("recaptcha")), &payload); err != nil || payload.Token != token || payload.TokenLength != len(token) || payload.TokenAge == nil || *payload.TokenAge != 0 {
						t.Error("encrypted request did not preserve the native SDK payload")
					}
				}
				if codeRequests <= tc.maxChallenges {
					_, _ = io.WriteString(w, `{"status":"fail","reason":"challenge","next_method":"`+tc.next+`","recaptcha_wait":0}`)
				} else {
					_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6}`)
				}
			}))
			defer server.Close()
			flow, _, _ := beginSyntheticFlow(t, server.Client())
			flow.http.baseURL, flow.http.serverKey = server.URL+"/", key.PublicKey().Bytes()
			flow.recaptchaSiteKey = "synthetic-site-key-123456"
			flow.recaptcha = recaptchaClient(func(ctx context.Context, request *waappv1.RecaptchaLoginTokenRequest) (*waappv1.RecaptchaLoginToken, error) {
				if request.GetSiteKey() != flow.recaptchaSiteKey {
					t.Error("SDK received device-owned registration configuration")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("SDK call has no deadline")
				}
				sdkCalls++
				return &waappv1.RecaptchaLoginToken{Token: token}, tc.providerError
			})
			if _, err = flow.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := flow.RequestCode(t.Context(), smsMethod)
			if (err != nil) != tc.wantError || result.GetStatus() != tc.wantStatus || codeRequests != tc.requests || sdkCalls != tc.proofs || flow.OutcomeUnknown() {
				t.Fatalf("status=%s error=%v requests=%d SDK=%d uncertain=%t", result.GetStatus(), err, codeRequests, sdkCalls, flow.OutcomeUnknown())
			}
			if errors.Is(err, ErrRegistrationState) {
				t.Fatal("known challenge discarded the active flow")
			}
		})
	}
}

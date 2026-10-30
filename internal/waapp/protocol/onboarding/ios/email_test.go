package ios

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

const emailMethod = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP

func TestEmailRegistrationKeepsSelectedChannelAndLeadingZero(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan url.Values, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		frame, err := base64.URLEncoding.DecodeString(r.PostForm.Get("ENC"))
		if err != nil {
			t.Error(err)
			return
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, key)
		if err != nil {
			t.Error(err)
			return
		}
		requests <- query
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":-1,"email_otp_eligible":1,"email_otp_wait":0}`)
		case "/code":
			if query.Get("method") != "email_otp" {
				t.Error("email request changed its verification channel")
			}
			_, _ = io.WriteString(w, `{"status":"sent","method":"email_otp","email_otp_length":8}`)
		case "/register":
			if query.Get("method") != "email_otp" || query.Get("code") != "01234567" {
				t.Error("email registration lost its channel or exact code")
			}
			_, _ = io.WriteString(w, `{"status":"ok","login":"synthetic-login","lid":"synthetic-lid"}`)
		default:
			t.Error("unexpected registration request")
		}
	}))
	defer server.Close()
	flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), server.Client(), Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close(t.Context())
	flow.http.baseURL, flow.http.serverKey = server.URL+"/", key.PublicKey().Bytes()
	if _, err := flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := flow.RequestCode(t.Context(), emailMethod)
	if err != nil || VerificationCodeLength(response, emailMethod) != 8 {
		t.Fatal("email response lost its selected code length", err)
	}
	if _, err := flow.SubmitCode(t.Context(), smsMethod, "01234567"); err == nil {
		t.Fatal("email verification accepted a different channel")
	}
	if _, err := flow.SubmitCode(t.Context(), emailMethod, "01234567"); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.KeyMaterial(); err != nil || len(requests) != 3 {
		t.Fatal("email verification did not complete once with its original keys", err)
	}
}

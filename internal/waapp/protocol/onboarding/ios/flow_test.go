package ios

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	stepx25519 "go.step.sm/crypto/x25519"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

const smsMethod = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS

func syntheticProfile() *protocolv1.IOSRegistrationProfile {
	return &protocolv1.IOSRegistrationProfile{
		AppVersion: "26.40.77", UserAgent: "WhatsApp/2.26.40.77 iOS/18.4.1",
		Language: "en", Locale: "GB", BuildTokenSecret: strings.Repeat("s", 40), BuildHash: strings.Repeat("0", 32),
		Client: &protocolv1.ClientMetadata{Platform: protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS, AppVersion: "2.26.40.77",
			OsVersion: "18.4.1", Device: "iPhone12,3", Manufacturer: "Apple", NoiseRootPublicKey: bytes.Repeat([]byte{1}, 32)},
	}
}

func TestFlowDegradesTemporaryKeyProofFailureWithoutCreatingAnotherKey(t *testing.T) {
	var contexts, attempts, posts atomic.Int32
	signer := attestClient(t, &attestServer{
		bind: func() *waappv1.AppAttestContext { contexts.Add(1); return syntheticAttestContext() },
		attest: func(*waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
			if attempts.Add(1) == 1 {
				return nil, status.Error(codes.Unavailable, "temporary SDK failure")
			}
			return syntheticAttestBinding(), nil
		},
		sign: func(_ context.Context, request *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
			return &waappv1.GenerateAppAttestAssertionResponse{Context: request.Context, Assertion: request.ClientDataHash}, nil
		},
	})
	client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.PostForm.Has("H") || request.Header.Get("Authorization") != "" {
			t.Fatal("unavailable proof produced attestation fields")
		}
		posts.Add(1)
		return jsonResponse(`{"status":"fail","reason":"incorrect","sms_wait":0}`), nil
	})}
	flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), client, Dependencies{Attestation: signer})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close(t.Context())
	if _, err := flow.Probe(t.Context()); err != nil || posts.Load() != 1 {
		t.Fatal("unavailable proof blocked the native unsigned request")
	}
	if _, err := flow.Probe(t.Context()); err != nil || contexts.Load() != 1 || attempts.Load() != 1 || posts.Load() != 2 {
		t.Fatal("degraded flow replaced its context or kept calling the worker")
	}
}

func syntheticPhone() *waappv1.PhoneTarget {
	return &waappv1.PhoneTarget{CountryCallingCode: "44", NationalNumber: "7700900000"}
}

func beginSyntheticFlow(t *testing.T, client *http.Client) (*Flow, <-chan []byte, *atomic.Int32) {
	t.Helper()
	bound := make(chan []byte, 1)
	released := new(atomic.Int32)
	signer := attestClient(t, &attestServer{
		bind: syntheticAttestContext,
		attest: func(request *waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
			bound <- bytes.Clone(request.ClientDataHash)
			return syntheticAttestBinding(), nil
		},
		sign: func(_ context.Context, request *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
			return &waappv1.GenerateAppAttestAssertionResponse{Context: request.Context, Assertion: request.ClientDataHash}, nil
		},
		release: func(*waappv1.AppAttestContext) { released.Add(1) },
	})
	flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), client, Dependencies{Attestation: signer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close(context.Background()) })
	return flow, bound, released
}

func openRegistrationRequest(request *http.Request, server *ecdh.PrivateKey) (url.Values, error) {
	if err := request.ParseForm(); err != nil {
		return nil, err
	}
	frame, err := base64.URLEncoding.DecodeString(request.PostForm.Get("ENC"))
	if err != nil || len(frame) < 48 {
		return nil, errors.New("invalid encrypted frame")
	}
	payload, err := base64.RawURLEncoding.DecodeString(request.PostForm.Get("H"))
	if err != nil {
		return nil, err
	}
	var proof struct{ Assertion []byte }
	if err := json.Unmarshal(payload, &proof); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(frame)
	if !bytes.Equal(proof.Assertion, hash[:]) {
		return nil, errors.New("proof does not bind this frame")
	}
	return protocoltest.OpenRegistrationFrame(frame, server)
}

func TestFlowRunsHostOwnedRegistrationOverTLS(t *testing.T) {
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan url.Values, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != "POST" || request.TLS == nil || request.Header.Get("Authorization") == "" ||
			request.Header.Get("User-Agent") != syntheticProfile().GetUserAgent() {
			t.Error("missing registration HTTP metadata or TLS")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		query, err := openRegistrationRequest(request, serverKey)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- query
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","sms_wait":0,"voice_wait":"0","ignored_server_field":true}`)
		case "/code":
			if query.Get("method") != "sms" {
				t.Error("delivery method is not SMS")
			}
			_, _ = io.WriteString(w, `{"status":"sent","method":"sms","length":6}`)
		case "/register":
			if query.Get("code") != "123456" {
				t.Error("verification code did not reach the encrypted request")
			}
			_, _ = io.WriteString(w, `{"status":"ok","login":"synthetic-login","lid":"synthetic-lid"}`)
		}
	}))
	defer server.Close()
	flow, bound, released := beginSyntheticFlow(t, server.Client())
	flow.http.baseURL, flow.http.serverKey = server.URL+"/", serverKey.PublicKey().Bytes()
	if _, err := flow.KeyMaterial(); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("unfinished flow exposed account key material")
	}
	probe, err := flow.Probe(t.Context())
	if err != nil || probe.GetReason() != "incorrect" || probe.SmsWait == nil || probe.VoiceWait == nil {
		t.Fatal("probe did not preserve number and string wait values")
	}
	code, err := flow.RequestCode(t.Context(), smsMethod)
	if err != nil || code.GetStatus() != "sent" {
		t.Fatal("code request failed")
	}
	if _, err := flow.SubmitCode(t.Context(), smsMethod, "１２３４５６"); err == nil {
		t.Fatal("non-ASCII code reached verification")
	}
	registered, err := flow.SubmitCode(t.Context(), smsMethod, "123456")
	if err != nil || registered.GetStatus() != "ok" || registered.GetLogin() != "synthetic-login" {
		t.Fatal("registration did not preserve the identity")
	}
	first := <-requests
	for range 2 {
		next := <-requests
		for _, key := range []string{"authkey", "e_ident", "e_skey_val", "e_skey_sig", "fdid", "expid", "access_session_id", "id", "ios_id"} {
			if first.Get(key) != next.Get(key) {
				t.Fatalf("registration changed the per-flow %s", key)
			}
		}
	}
	noise, _ := base64.URLEncoding.DecodeString(first.Get("authkey"))
	wantHash := sha256.Sum256(noise)
	identity, _ := base64.URLEncoding.DecodeString(first.Get("e_ident"))
	prekey, _ := base64.URLEncoding.DecodeString(first.Get("e_skey_val"))
	signature, _ := base64.URLEncoding.DecodeString(first.Get("e_skey_sig"))
	if !bytes.Equal(<-bound, wantHash[:]) || !stepx25519.Verify(identity, append([]byte{5}, prekey...), signature) || len(first.Get("id")) != 16 {
		t.Fatal("host keys, signed prekey, recovery identifier or proof binding is invalid")
	}
	material, err := flow.KeyMaterial()
	if err != nil {
		t.Fatal(err)
	}
	material.NoisePrivateKey[0] ^= 255
	if bytes.Equal(material.GetNoisePrivateKey(), flow.keys.GetNoisePrivateKey()) {
		t.Fatal("handoff material aliases active flow state")
	}
	if err := flow.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := flow.Close(t.Context()); err != nil || released.Load() != 1 || flow.keys != nil || flow.authorization != "" {
		t.Fatal("close did not release and clear the flow exactly once")
	}
}

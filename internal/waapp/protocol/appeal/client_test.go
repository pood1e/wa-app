package appeal

import (
	"context"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/secretbox"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func TestEncryptedReviewHTTPFlow(t *testing.T) {
	ik, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ek, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	const token = "synthetic-account-review-token"
	var clientPublic string
	calls := []int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Variables string `json:"variables"`
			Token     string `json:"access_token"`
			Document  int64  `json:"doc_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Token != "synthetic-application" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid GraphQL JSON envelope")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls = append(calls, request.Document)
		w.Header().Set("Content-Type", "application/json")
		switch request.Document {
		case statusDocument:
			var variables struct{ AppID, Token string }
			var fields map[string]string
			if err := json.Unmarshal([]byte(request.Variables), &fields); err != nil {
				t.Error(err)
			}
			variables.AppID, variables.Token = fields["app_id"], fields["request_token"]
			if variables.AppID != applicationID || variables.Token != token {
				t.Error("status query lost account authorization")
			}
			_, _ = io.WriteString(w, `{"data":{"whatsapp_support_ban_appeal_status":{"status":"NO_APPEAL_OPENED"}}}`)
		case keysDocument:
			var variables struct {
				Public string `json:"client_pub_key"`
			}
			if err := json.Unmarshal([]byte(request.Variables), &variables); err != nil {
				t.Error(err)
			}
			clientPublic = variables.Public
			keys := serverKeys{Identity: base64.URLEncoding.EncodeToString(ik.PublicKey().Bytes()), Ephemeral: base64.URLEncoding.EncodeToString(ek.PublicKey().Bytes()), Nonce: base64.URLEncoding.EncodeToString(nonce[:])}
			if err := json.NewEncoder(w).Encode(struct {
				Data *responseData `json:"data"`
			}{&responseData{Keys: &keys}}); err != nil {
				t.Error(err)
			}
		case submitDocument:
			var variables struct {
				Request encryptedRequest `json:"user_request"`
			}
			if err := json.Unmarshal([]byte(request.Variables), &variables); err != nil {
				t.Error(err)
			}
			if variables.Request.ClientKey != clientPublic || strings.Contains(request.Variables, token) {
				t.Error("submission reused a different key or exposed plaintext authorization")
			}
			plain, err := openSyntheticReview(ik, ek, nonce, variables.Request)
			var payload struct {
				Token       string `json:"token"`
				Description string `json:"description"`
			}
			if err != nil || json.Unmarshal(plain, &payload) != nil || payload.Token != token || payload.Description != "synthetic review description" {
				t.Error("server could not authenticate and decode the review")
			}
			_, _ = io.WriteString(w, `{"data":{"whatsapp_support_process_ban_appeal_request_encrypted":{"status":"IN_REVIEW"}}}`)
		default:
			t.Error("unexpected GraphQL document")
		}
	}))
	defer server.Close()
	client, err := New(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint, client.application.AccessToken = server.URL, "synthetic-application"
	metadata := &protocolv1.ClientMetadata{Platform: protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS, AppVersion: "26.40.77", OsVersion: "18.4.1"}
	status, err := client.Status(t.Context(), token, metadata)
	if err != nil || status != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_NOT_REQUESTED {
		t.Fatal("status query failed")
	}
	status, err = client.Submit(t.Context(), token, "synthetic review description", metadata)
	if err != nil || status != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW || len(calls) != 3 {
		t.Fatal("encrypted review did not reach the review state in three requests")
	}
}

func openSyntheticReview(ik, ek *ecdh.PrivateKey, nonce [24]byte, request encryptedRequest) ([]byte, error) {
	public, err := base64.RawStdEncoding.DecodeString(request.ClientKey)
	if err != nil {
		return nil, err
	}
	peer, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return nil, err
	}
	first, err := ek.ECDH(peer)
	if err != nil {
		return nil, err
	}
	second, err := ik.ECDH(peer)
	if err != nil {
		return nil, err
	}
	material := append(first, second...)
	material = append(material, ek.PublicKey().Bytes()...)
	material = append(material, public...)
	key, err := hkdf.Key(sha256.New, material, nil, "WA_INAPP_BAN_APPEALS", 32)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.URLEncoding.DecodeString(request.Ciphertext)
	if err != nil {
		return nil, err
	}
	plain, ok := secretbox.Open(nil, ciphertext, &nonce, (*[32]byte)(key))
	if !ok {
		return nil, fmt.Errorf("synthetic server rejected ciphertext")
	}
	return plain, nil
}

func TestReviewErrorsDoNotExposeResponseOrRetrySubmission(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"errors":[{"code":2498023,"message":"synthetic-secret-token"}]}`)
	}))
	defer server.Close()
	client, err := New(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint, client.application.AccessToken = server.URL, "synthetic-application"
	_, err = client.Status(context.Background(), "synthetic-account", nil)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret-token") || calls != 1 {
		t.Fatal("upstream body leaked or request was automatically retried")
	}
}

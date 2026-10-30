package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"google.golang.org/protobuf/proto"
)

func TestCanonicalCredentialsCreateRefreshAndPreservePrivateState(t *testing.T) {
	server, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&server.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	now := time.Unix(1800000000, 0)
	calls := []Operation{}
	credentialPassword := ""
	mex := func(_ context.Context, op Operation, variables any) (json.RawMessage, error) {
		calls = append(calls, op)
		if op == TriggerRecovery {
			return json.RawMessage(`{"xwa2_ent_request_recovery_nonce":{"fbid":"9001"}}`), nil
		}
		if op == ExchangeNonce {
			return nil, graph.ResponseError{Code: 482}
		}
		if op == GetCertificates {
			return json.Marshal(struct {
				Certificates any `json:"xwa2_ent_get_certificates"`
			}{struct {
				Encryption any `json:"encryption_pem"`
				Password   any `json:"password_pem"`
			}{struct {
				PEM string `json:"pem"`
			}{public}, struct {
				PEM string `json:"pem"`
				ID  int    `json:"key_id"`
			}{public, 7}}})
		}
		raw, err := json.Marshal(variables)
		if err != nil {
			return nil, err
		}
		var input struct {
			Input struct {
				Encrypted *encryptedValue `json:"encrypted_password"`
				User      string          `json:"encrypted_fbid"`
			} `json:"input"`
		}
		if json.Unmarshal(raw, &input) != nil {
			return nil, errors.New("invalid credential input")
		}
		plain, err := openCanonical(input.Input.Encrypted, server)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Version  int    `json:"version"`
			Password string `json:"password"`
			Public   string `json:"client_pub_key"`
			Type     string `json:"client_pub_key_type"`
			Time     int64  `json:"timestamp"`
		}
		if json.Unmarshal(plain, &envelope) != nil || envelope.Version != 1 || envelope.Type != "RSA 2048" || envelope.Time != now.Unix() {
			return nil, errors.New("canonical encryption did not preserve native client parameters")
		}
		password, err := protocoltest.OpenPassword(server, envelope.Password)
		if err != nil {
			return nil, err
		}
		if len(password) != 50 {
			return nil, errors.New("canonical credential secret has the wrong length")
		}
		if op == CreateUser {
			credentialPassword = password
		} else if op != CreateAccessToken || input.Input.User != "9001" || password != credentialPassword {
			return nil, errors.New("refresh changed the existing canonical identity or secret")
		}
		client, err := parsePublicKey(envelope.Public)
		if err != nil {
			return nil, err
		}
		fields, err := json.Marshal(struct {
			User  int64  `json:"fbid"`
			Token string `json:"access_token"`
			Time  int64  `json:"timestamp"`
			TTL   int64  `json:"ttl"`
		}{9001, "synthetic-canonical-token", now.Unix(), 3600})
		if err != nil {
			return nil, err
		}
		body := struct {
			Data string `json:"data"`
		}{string(fields)}
		plain, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		sealed, err := sealCanonical(plain, client)
		if err != nil {
			return nil, err
		}
		field := "xwa2_ent_create_ent"
		if op == CreateAccessToken {
			field = "xwa2_ent_generate_access_tokens"
		}
		return json.Marshal(map[string]any{field: struct {
			Encrypted *encryptedValue `json:"encrypted_fbid_and_access_token"`
		}{sealed}})
	}
	client := New(mex, nil, nil, func() time.Time { return now }, func(context.Context) (string, error) { return "synthetic-nonce", nil })
	created, err := client.Credentials(t.Context(), nil)
	if err != nil || created.GetUserId() != "9001" || created.GetExpiresAt() != now.Add(time.Hour).Unix() || created.GetCredentialPassword() != credentialPassword {
		t.Fatal("canonical credentials were not recovered from the authenticated ciphertext")
	}
	cached, err := client.Credentials(t.Context(), created)
	if err != nil || cached != created || len(calls) != 4 {
		t.Fatal("valid canonical credentials were recreated")
	}
	before := proto.Clone(created).(*protocolv1.CanonicalCredentials)
	now = now.Add(time.Hour)
	refreshed, err := client.Credentials(t.Context(), created)
	if err != nil || refreshed == created || !proto.Equal(before, created) || refreshed.GetUserId() != created.GetUserId() || refreshed.GetCredentialPassword() != created.GetCredentialPassword() {
		t.Fatal("canonical refresh replaced or mutated the existing credential identity")
	}
	if len(calls) != 6 || calls[5] != CreateAccessToken {
		t.Fatal("expired token refresh recreated the canonical user")
	}
}

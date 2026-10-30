package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
)

func TestCanonicalRecoveryDecryptsNonceAndOnlyCreatesAfter482(t *testing.T) {
	for _, code := range []int{0, 482, 400, 401, 429} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var challenge string
			calls := 0
			mex := func(_ context.Context, op Operation, variables any) (json.RawMessage, error) {
				calls++
				raw, err := json.Marshal(variables)
				if err != nil {
					return nil, err
				}
				var input struct {
					Input struct {
						Key     string `json:"nonce_encryption_key"`
						Nonce   string `json:"nonce"`
						UseCase string `json:"use_case"`
					} `json:"input"`
				}
				if json.Unmarshal(raw, &input) != nil || input.Input.UseCase != "CANONICAL" {
					return nil, errors.New("recovery did not use the canonical account")
				}
				if op == TriggerRecovery {
					public, err := base64.StdEncoding.DecodeString(input.Input.Key)
					if err != nil {
						return nil, err
					}
					key, err := parsePublicKey(string(public))
					if err != nil {
						return nil, err
					}
					encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, []byte("synthetic-recovery-nonce"), nil)
					challenge = string(encrypted)
					return json.RawMessage(`{"xwa2_ent_request_recovery_nonce":{"fbid":"9001"}}`), err
				}
				if op != ExchangeNonce || input.Input.Nonce != "synthetic-recovery-nonce" {
					return nil, errors.New("recovery exchanged an unverified challenge")
				}
				if code != 0 {
					return nil, graph.ResponseError{Code: code}
				}
				return json.RawMessage(`{"xwa2_ent_exchange_nonce_and_password":{"encrypted_fbid_and_access_token":{"algorithm":"rsa2048","data":"synthetic"}}}`), nil
			}
			client := New(mex, nil, nil, time.Now, func(context.Context) (string, error) { return challenge, nil })
			value, create, err := client.recoverCanonical(t.Context(), &encryptedValue{Algorithm: "rsa2048"})
			if calls != 2 || create != (code == 482) || (err != nil) != (code != 0 && code != 482) {
				t.Fatal("recovery created a user without an explicit invalid-nonce response")
			}
			if code == 0 && (value == nil || value.Data != "synthetic") {
				t.Fatal("recovery discarded the encrypted credential response")
			}
		})
	}
}

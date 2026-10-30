package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"

	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
)

// recoverCanonical replaces the canonical credential secret through the owned account connection.
// Native clients create a user only when nonce exchange explicitly reports 482.
func (c *Client) recoverCanonical(ctx context.Context, password *encryptedValue) (*encryptedValue, bool, error) {
	if c.nonce == nil {
		return nil, false, errors.New("canonical recovery requires the active account connection")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, false, err
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, false, err
	}
	input := struct {
		Key     string `json:"nonce_encryption_key"`
		UseCase string `json:"use_case"`
	}{base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "CANONICAL"}
	if _, err := c.mex(ctx, TriggerRecovery, variables(input)); err != nil {
		return nil, false, err
	}
	nonce, err := c.nonce(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(nonce) == key.Size() {
		plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, []byte(nonce), nil)
		if err != nil {
			return nil, false, errors.New("canonical recovery nonce could not be decrypted")
		}
		nonce = string(plain)
	}
	if nonce == "" {
		return nil, false, errors.New("canonical recovery nonce is empty")
	}
	response, err := query[struct {
		Result *struct {
			Encrypted *encryptedValue `json:"encrypted_fbid_and_access_token"`
		} `json:"xwa2_ent_exchange_nonce_and_password"`
	}](ctx, c, ExchangeNonce, variables(struct {
		Nonce    string          `json:"nonce"`
		UseCase  string          `json:"use_case"`
		Password *encryptedValue `json:"encrypted_password"`
	}{nonce, "CANONICAL", password}))
	if err != nil {
		var rejected graph.ResponseError
		if errors.As(err, &rejected) && (rejected.Code == 482 || rejected.Extensions.Code == 482) {
			return nil, true, nil
		}
		return nil, false, err
	}
	if response.Result == nil || response.Result.Encrypted == nil {
		return nil, false, errors.New("canonical recovery credential response is missing")
	}
	return response.Result.Encrypted, false, nil
}

func canonicalResponse(data json.RawMessage, operation Operation) (*encryptedValue, error) {
	type result struct {
		Value *encryptedValue `json:"encrypted_fbid_and_access_token"`
	}
	var response struct {
		Created   *result `json:"xwa2_ent_create_ent"`
		Refreshed *result `json:"xwa2_ent_generate_access_tokens"`
	}
	if json.Unmarshal(data, &response) != nil {
		return nil, errors.New("canonical credential response is invalid")
	}
	value := response.Created
	if operation == CreateAccessToken {
		value = response.Refreshed
	}
	if value == nil || value.Value == nil {
		return nil, errors.New("canonical credential response is missing")
	}
	return value.Value, nil
}

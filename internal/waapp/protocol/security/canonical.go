package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strconv"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"google.golang.org/protobuf/proto"
)

func (c *Client) Credentials(ctx context.Context, previous *protocolv1.CanonicalCredentials) (*protocolv1.CanonicalCredentials, error) {
	if previous.GetAccessToken() != "" && (previous.GetExpiresAt() == 0 || previous.GetExpiresAt() > c.now().Add(time.Minute).Unix()) {
		return previous, nil
	}
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	certs, err := query[struct {
		Certificates *struct {
			Encryption *struct {
				PEM string `json:"pem"`
			} `json:"encryption_pem"`
			Password *struct {
				PEM string `json:"pem"`
				ID  int    `json:"key_id"`
			} `json:"password_pem"`
		} `json:"xwa2_ent_get_certificates"`
	}](ctx, c, GetCertificates, variables(struct {
		Time    string `json:"request_time"`
		UseCase string `json:"use_case"`
	}{timestamp, "CANONICAL"}))
	if err != nil {
		return nil, err
	}
	if certs.Certificates == nil || certs.Certificates.Encryption == nil || certs.Certificates.Password == nil {
		return nil, errors.New("canonical encryption certificates are missing")
	}
	encryption, err := parsePublicKey(certs.Certificates.Encryption.PEM)
	if err != nil {
		return nil, err
	}
	passwordKey, err := parsePublicKey(certs.Certificates.Password.PEM)
	if err != nil {
		return nil, err
	}
	credentials := new(protocolv1.CanonicalCredentials)
	if previous != nil {
		credentials = proto.Clone(previous).(*protocolv1.CanonicalCredentials)
	}
	if credentials.GetCredentialPassword() == "" {
		credentials.CredentialPassword, err = newCredentialPassword()
		if err != nil {
			return nil, err
		}
	}
	client, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	public, err := x509.MarshalPKIXPublicKey(&client.PublicKey)
	if err != nil {
		return nil, err
	}
	sealedPassword, err := encryptPassword(credentials.GetCredentialPassword(), passwordKey, certs.Certificates.Password.ID, c.now().Unix())
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(struct {
		Version       int    `json:"version"`
		Time          int64  `json:"timestamp"`
		Password      string `json:"password"`
		PublicKey     string `json:"client_pub_key"`
		PublicKeyType string `json:"client_pub_key_type"`
	}{1, c.now().Unix(), sealedPassword, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "RSA 2048"})
	if err != nil {
		return nil, err
	}
	sealed, err := sealCanonical(plain, encryption)
	if err != nil {
		return nil, err
	}
	input := struct {
		Password *encryptedValue `json:"encrypted_password"`
		Time     string          `json:"timestamp"`
		UseCase  string          `json:"use_case"`
		UserID   string          `json:"encrypted_fbid,omitempty"`
	}{sealed, timestamp, "CANONICAL", credentials.GetUserId()}
	op := CreateUser
	if input.UserID != "" {
		op = CreateAccessToken
	}
	var value *encryptedValue
	create := true
	if op == CreateUser {
		value, create, err = c.recoverCanonical(ctx, sealed)
		if err != nil {
			return nil, err
		}
	}
	if create {
		data, err := c.mex(ctx, op, variables(input))
		if err != nil {
			return nil, err
		}
		value, err = canonicalResponse(data, op)
		if err != nil {
			return nil, err
		}
	}
	plain, err = openCanonical(value, client)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data string `json:"data"`
	}
	if json.Unmarshal(plain, &envelope) != nil || envelope.Data == "" {
		return nil, errors.New("canonical credential envelope is incomplete")
	}
	var decoded struct {
		UserID json.Number `json:"fbid"`
		Token  string      `json:"access_token"`
		Time   int64       `json:"timestamp"`
		TTL    int64       `json:"ttl"`
	}
	if json.Unmarshal([]byte(envelope.Data), &decoded) != nil || decoded.UserID == "" || decoded.Token == "" {
		return nil, errors.New("canonical credential payload is incomplete")
	}
	userID, err := decoded.UserID.Int64()
	if err != nil || userID <= 0 || (credentials.GetUserId() != "" && credentials.GetUserId() != string(decoded.UserID)) {
		return nil, errors.New("canonical credential identity is invalid")
	}
	credentials.UserId = string(decoded.UserID)
	credentials.AccessToken = decoded.Token
	credentials.ExpiresAt = 0
	if decoded.TTL > 0 && decoded.Time > 0 {
		credentials.ExpiresAt = decoded.Time + decoded.TTL
	}
	return credentials, nil
}

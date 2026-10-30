package security

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// EncryptPassword fetches the current canonical public key and seals the password locally.
func (c *Client) EncryptPassword(ctx context.Context, password, accessToken string) (string, error) {
	key, err := c.PasswordEncryptionKey(ctx, accessToken)
	if err != nil {
		return "", err
	}
	return SealPassword(password, key, c.now())
}

// PasswordEncryptionKey returns public material without exporting account authorization.
func (c *Client) PasswordEncryptionKey(ctx context.Context, accessToken string) (*protocolv1.PasswordEncryptionKey, error) {
	data, err := c.post(ctx, 27318258844468227, struct{}{}, accessToken)
	if err != nil {
		return nil, err
	}
	var encryption struct {
		Key *protocolv1.PasswordEncryptionKey `json:"wa_password_encryption_key"`
	}
	if json.Unmarshal(data, &encryption) != nil || encryption.Key == nil {
		return nil, errors.New("account password encryption key is missing")
	}
	if _, err = parsePublicKey(encryption.Key.GetPublicKey()); err != nil {
		return nil, err
	}
	if encryption.Key.GetKeyId() < 1 || encryption.Key.GetKeyId() > 255 {
		return nil, errors.New("account password key ID is invalid")
	}
	return encryption.Key, nil
}

// SealPassword preserves the exact password inside the native canonical envelope.
func SealPassword(password string, encryption *protocolv1.PasswordEncryptionKey, now time.Time) (string, error) {
	key, err := parsePublicKey(encryption.GetPublicKey())
	if err != nil {
		return "", err
	}
	return encryptPassword(password, key, int(encryption.GetKeyId()), now.Unix())
}

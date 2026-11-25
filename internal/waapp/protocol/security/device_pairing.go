package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

// PairingMetadata seals the primary's generated credential secret for the server.
func (c *Client) PairingMetadata(ctx context.Context, credentials *protocolv1.CanonicalCredentials) (wire.ChatdNode, error) {
	if credentials.GetUserId() == "" || credentials.GetCredentialPassword() == "" {
		return wire.ChatdNode{}, errors.New("device linking requires canonical account credentials")
	}
	keys, err := c.canonicalKeys(ctx)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	password, err := encryptPassword(credentials.GetCredentialPassword(), keys.password, keys.passwordID, c.now().Unix())
	if err != nil {
		return wire.ChatdNode{}, err
	}
	plain, err := json.Marshal(struct {
		Version   int    `json:"version"`
		Timestamp int64  `json:"timestamp"`
		Password  string `json:"password"`
		PublicKey string `json:"client_pub_key"`
		KeyType   string `json:"client_pub_key_type"`
	}{1, c.now().Unix(), password, keys.passwordPEM, "RSA 2048"})
	if err != nil {
		return wire.ChatdNode{}, err
	}
	defer clear(plain)
	sealed, err := sealCanonical(plain, keys.encryption)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	children := make([]wire.ChatdNode, 0, 4)
	for _, value := range []struct{ tag, data string }{{"encrypted_key", sealed.Key}, {"nonce", sealed.Nonce}, {"encrypted_data", sealed.Data}, {"auth_tag", sealed.Tag}} {
		raw, err := base64.StdEncoding.DecodeString(value.data)
		if err != nil {
			return wire.ChatdNode{}, errors.New("device linking encryption metadata is invalid")
		}
		children = append(children, wire.ChatdNode{Tag: value.tag, Content: raw})
	}
	return wire.ChatdNode{Tag: "encryption-metadata", Attrs: map[string]string{"version": "1", "algorithm": sealed.Algorithm}, Content: children}, nil
}

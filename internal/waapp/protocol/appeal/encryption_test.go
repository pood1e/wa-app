package appeal

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestEncryptionMatchesIndependentVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/encryption.json")
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic vector produced with Python cryptography and libsodium.
	var fixture struct {
		Private    []byte `json:"client_private"`
		Identity   string `json:"identity"`
		Ephemeral  string `json:"ephemeral"`
		Nonce      string `json:"nonce"`
		Plaintext  string `json:"plaintext"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(fixture.Private)
	if err != nil {
		t.Fatal(err)
	}
	keys := serverKeys{Identity: fixture.Identity, Ephemeral: fixture.Ephemeral, Nonce: fixture.Nonce}
	result, err := encrypt(private, keys, []byte(fixture.Plaintext))
	if err != nil || result.Ciphertext != fixture.Ciphertext || result.Nonce != fixture.Nonce {
		t.Fatal("account review ciphertext differs from independent vector")
	}
	if strings.Contains(result.ClientKey, "=") {
		t.Fatal("client public key retained Base64 padding")
	}
	for name, modify := range map[string]func(*serverKeys){
		"low_order_ephemeral": func(keys *serverKeys) { keys.Ephemeral = base64.URLEncoding.EncodeToString(make([]byte, 32)) },
		"invalid_identity":    func(keys *serverKeys) { keys.Identity = "invalid" },
		"invalid_nonce":       func(keys *serverKeys) { keys.Nonce = "invalid" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := keys
			modify(&invalid)
			if _, err := encrypt(private, invalid, []byte(fixture.Plaintext)); err == nil {
				t.Fatal("invalid server material was accepted")
			}
		})
	}
}

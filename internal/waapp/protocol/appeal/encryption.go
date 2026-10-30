package appeal

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/nacl/secretbox"
)

type serverKeys struct {
	Ephemeral string `json:"purpose_public_ek"`
	Identity  string `json:"purpose_public_ik"`
	Nonce     string `json:"purpose_dummy_nonce"`
}

type encryptedRequest struct {
	ClientKey  string `json:"client_ephemeral_pub_key_base64"`
	Ciphertext string `json:"ciphertext_base64"`
	Nonce      string `json:"encryption_nonce_base64"`
}

func encrypt(private *ecdh.PrivateKey, keys serverKeys, plaintext []byte) (*encryptedRequest, error) {
	ephemeral, err := decodeMaterial(keys.Ephemeral, 32)
	if err != nil {
		return nil, err
	}
	identity, err := decodeMaterial(keys.Identity, 32)
	if err != nil {
		return nil, err
	}
	nonce, err := decodeMaterial(keys.Nonce, 24)
	if err != nil {
		return nil, err
	}
	material := make([]byte, 0, 128)
	for _, public := range [][]byte{ephemeral, identity} {
		peer, err := ecdh.X25519().NewPublicKey(public)
		if err != nil {
			return nil, errors.New("invalid account review public key")
		}
		shared, err := private.ECDH(peer)
		if err != nil {
			return nil, errors.New("account review key agreement failed")
		}
		material = append(material, shared...)
	}
	material = append(material, ephemeral...)
	material = append(material, private.PublicKey().Bytes()...)
	derived, err := hkdf.Key(sha256.New, material, nil, "WA_INAPP_BAN_APPEALS", 64)
	if err != nil {
		return nil, errors.New("account review key derivation failed")
	}
	ciphertext := secretbox.Seal(nil, plaintext, (*[24]byte)(nonce), (*[32]byte)(derived[:32]))
	return &encryptedRequest{
		ClientKey:  base64.RawStdEncoding.EncodeToString(private.PublicKey().Bytes()),
		Ciphertext: base64.URLEncoding.EncodeToString(ciphertext), Nonce: keys.Nonce,
	}, nil
}

func decodeMaterial(value string, size int) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := encoding.DecodeString(value); err == nil && len(raw) == size {
			return raw, nil
		}
	}
	return nil, errors.New("invalid account review key material")
}

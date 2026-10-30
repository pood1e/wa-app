// Package wire implements shared native protocol encodings.
package wire

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
)

// SealRegistration returns the binary ENC envelope with a fresh X25519 key.
// The zero nonce belongs to this wire format; each call derives a new AES key.
func SealRegistration(plaintext, serverPublicKey []byte) ([]byte, error) {
	server, err := ecdh.X25519().NewPublicKey(serverPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse registration server key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate registration ephemeral key: %w", err)
	}
	return sealRegistration(plaintext, server, ephemeral)
}

func sealRegistration(plaintext []byte, server *ecdh.PublicKey, ephemeral *ecdh.PrivateKey) ([]byte, error) {
	shared, err := ephemeral.ECDH(server)
	if err != nil {
		return nil, fmt.Errorf("derive registration encryption key: %w", err)
	}
	block, err := aes.NewCipher(shared)
	if err != nil {
		return nil, fmt.Errorf("create registration cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create registration AEAD: %w", err)
	}
	frame := ephemeral.PublicKey().Bytes()
	frame = gcm.Seal(frame, make([]byte, gcm.NonceSize()), plaintext, nil)
	return frame, nil
}

package protocoltest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"errors"
	"net/url"
)

// OpenRegistrationFrame independently decodes a captured registration envelope.
func OpenRegistrationFrame(frame []byte, server *ecdh.PrivateKey) (url.Values, error) {
	if len(frame) < 48 {
		return nil, errors.New("invalid registration frame")
	}
	public, err := ecdh.X25519().NewPublicKey(frame[:32])
	if err != nil {
		return nil, err
	}
	shared, err := server.ECDH(public)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(shared)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, make([]byte, gcm.NonceSize()), frame[32:], nil)
	if err != nil {
		return nil, err
	}
	return url.ParseQuery(string(plain))
}

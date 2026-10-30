package wire

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"fmt"

	stepx25519 "go.step.sm/crypto/x25519"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// NewRegistrationKeys generates independent Noise, Signal identity and signed prekeys.
func NewRegistrationKeys() (*protocolv1.RegistrationKeyMaterial, error) {
	noise, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Noise key: %w", err)
	}
	_, identity, err := stepx25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Signal identity: %w", err)
	}
	signedPublic, signed, err := stepx25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signed prekey: %w", err)
	}
	signature, err := stepx25519.Sign(rand.Reader, identity, append([]byte{5}, signedPublic...))
	if err != nil {
		return nil, fmt.Errorf("sign prekey: %w", err)
	}
	var ids [8]byte
	if _, err := rand.Read(ids[:]); err != nil {
		return nil, fmt.Errorf("generate registration identifiers: %w", err)
	}
	return &protocolv1.RegistrationKeyMaterial{
		NoisePrivateKey: noise.Bytes(), IdentityPrivateKey: identity,
		SignedPreKeyPrivateKey: signed, SignedPreKeySignature: signature,
		RegistrationId: binary.BigEndian.Uint32(ids[:4])%16380 + 1,
		SignedPreKeyId: binary.BigEndian.Uint32(ids[4:])%(1<<24-1) + 1,
	}, nil
}

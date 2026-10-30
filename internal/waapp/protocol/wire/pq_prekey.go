package wire

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"errors"

	"github.com/cloudflare/circl/kem/mlkem/mlkem1024"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// NewSignalPQPreKey generates the ML-KEM-1024 bundle used by Android registration.
func NewSignalPQPreKey(identityPrivate []byte, id int32) (*protocolv1.SignalPQPreKey, error) {
	if id < 1 || id > 0xffffff {
		return nil, errors.New("invalid PQ prekey ID")
	}
	public, private, err := mlkem1024.GenerateKeyPair(rand.Reader)
	if err != nil {
		return nil, errors.New("generate PQ prekey")
	}
	publicBytes, err := public.MarshalBinary()
	if err != nil {
		return nil, errors.New("encode PQ public key")
	}
	privateBytes, err := private.MarshalBinary()
	if err != nil {
		return nil, errors.New("encode PQ private key")
	}
	signature, err := XeddsaSignCurve25519(identityPrivate, publicBytes)
	if err != nil {
		clear(privateBytes)
		return nil, errors.New("sign PQ prekey")
	}
	return &protocolv1.SignalPQPreKey{Id: id, Public: publicBytes, Private: privateBytes, Signature: signature}, nil
}

// ValidateSignalPQPreKey checks imported private material and its identity signature.
func ValidateSignalPQPreKey(key *protocolv1.SignalPQPreKey, identityPublic []byte) error {
	if key.GetId() < 1 || key.GetId() > 0xffffff || len(key.GetPublic()) != mlkem1024.PublicKeySize ||
		len(key.GetPrivate()) != mlkem1024.PrivateKeySize || len(key.GetSignature()) != 64 {
		return errors.New("invalid PQ prekey material")
	}
	scheme := mlkem1024.Scheme()
	private, err := scheme.UnmarshalBinaryPrivateKey(key.GetPrivate())
	if err != nil {
		return errors.New("invalid PQ private key")
	}
	public, err := private.Public().MarshalBinary()
	if err != nil || !bytes.Equal(public, key.GetPublic()) {
		return errors.New("PQ key pair does not match")
	}
	encapsulationKey, err := scheme.UnmarshalBinaryPublicKey(public)
	if err != nil {
		return errors.New("invalid PQ public key")
	}
	ciphertext, shared, err := scheme.Encapsulate(encapsulationKey)
	if err != nil {
		return errors.New("validate PQ encapsulation")
	}
	actual, err := scheme.Decapsulate(private, ciphertext)
	if err != nil || subtle.ConstantTimeCompare(actual, shared) != 1 {
		return errors.New("PQ key pair does not match")
	}
	valid, err := XeddsaVerifyCurve25519(identityPublic, key.GetPublic(), key.GetSignature())
	if err != nil || !valid {
		return errors.New("invalid PQ prekey signature")
	}
	return nil
}

// DecapsulateSignalPQ handles raw Android ML-KEM-1024 ciphertext and expanded keys.
func DecapsulateSignalPQ(private, ciphertext []byte) ([]byte, error) {
	if len(private) != mlkem1024.PrivateKeySize || len(ciphertext) != mlkem1024.CiphertextSize {
		return nil, errors.New("invalid PQ encapsulation lengths")
	}
	scheme := mlkem1024.Scheme()
	key, err := scheme.UnmarshalBinaryPrivateKey(private)
	if err != nil {
		return nil, errors.New("invalid PQ private key")
	}
	shared, err := scheme.Decapsulate(key, ciphertext)
	if err != nil {
		return nil, errors.New("decapsulate PQ prekey message")
	}
	return shared, nil
}

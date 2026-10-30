package wire

import (
	"crypto/sha256"
)

type NoiseXXState struct {
	ChainingKey   []byte
	HandshakeHash []byte
	CipherKey     []byte
	Nonce         uint64
}

func NewNoiseState(handshakeName []byte) *NoiseXXState {
	name := NormalizeNoiseHandshakeName(handshakeName)
	state := &NoiseXXState{ChainingKey: append([]byte{}, name...), HandshakeHash: append([]byte{}, name...)}
	state.MixHash([]byte(WaNoisePrologue))
	return state
}

func NormalizeNoiseHandshakeName(handshakeName []byte) []byte {
	if len(handshakeName) > 32 {
		sum := sha256.Sum256(handshakeName)
		return append([]byte{}, sum[:]...)
	}
	out := make([]byte, 32)
	copy(out, handshakeName)
	return out
}

func (s *NoiseXXState) MixHash(data []byte) {
	sum := sha256.Sum256(append(append([]byte{}, s.HandshakeHash...), data...))
	s.HandshakeHash = sum[:]
}

func (s *NoiseXXState) MixKey(inputKeyMaterial []byte) {
	expanded := HkdfExtractExpand(s.ChainingKey, inputKeyMaterial, nil, 64)
	s.ChainingKey = append([]byte{}, expanded[:32]...)
	s.CipherKey = append([]byte{}, expanded[32:64]...)
	s.Nonce = 0
}

func (s *NoiseXXState) EncryptAndHash(plaintext []byte) ([]byte, error) {
	ciphertext := append([]byte{}, plaintext...)
	var err error
	if s.CipherKey != nil {
		ciphertext, err = AesGCMSeal(s.CipherKey, NativeNonce(s.Nonce), plaintext, s.HandshakeHash)
		if err != nil {
			return nil, err
		}
		s.Nonce++
	}
	s.MixHash(ciphertext)
	return ciphertext, nil
}

func (s *NoiseXXState) DecryptAndHash(ciphertext []byte) ([]byte, error) {
	plaintext := append([]byte{}, ciphertext...)
	var err error
	if s.CipherKey != nil {
		plaintext, err = AesGCMOpen(s.CipherKey, NativeNonce(s.Nonce), ciphertext, s.HandshakeHash)
		if err != nil {
			return nil, err
		}
		s.Nonce++
	}
	s.MixHash(ciphertext)
	return plaintext, nil
}

func (s *NoiseXXState) Split() ([]byte, []byte) {
	expanded := HkdfExtractExpand(s.ChainingKey, nil, nil, 64)
	return append([]byte{}, expanded[:32]...), append([]byte{}, expanded[32:64]...)
}

type ChatdTransportKeys struct {
	SendKey              []byte
	RecvKey              []byte
	ServerStaticPublic   []byte
	ServerStaticPQPublic []byte
	SendCounter          uint64
	RecvCounter          uint64
}

func (k *ChatdTransportKeys) Encrypt(plaintext []byte) ([]byte, error) {
	ciphertext, err := AesGCMSeal(k.SendKey, NativeNonce(k.SendCounter), plaintext, nil)
	if err != nil {
		return nil, err
	}
	k.SendCounter++
	return ciphertext, nil
}

func (k *ChatdTransportKeys) Decrypt(ciphertext []byte) ([]byte, error) {
	plaintext, err := AesGCMOpen(k.RecvKey, NativeNonce(k.RecvCounter), ciphertext, nil)
	if err != nil {
		return nil, err
	}
	k.RecvCounter++
	return plaintext, nil
}

type LoginIdentity struct {
	Jid      string
	Username uint64
}

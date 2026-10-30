package wire_test

import (
	"bytes"
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"testing"

	"github.com/cloudflare/circl/kem/mlkem/mlkem1024"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestSignalPQEncodingInteroperatesWithStandardLibrary(t *testing.T) {
	standard, err := mlkem.GenerateKey1024()
	if err != nil {
		t.Fatal(err)
	}
	public, private := mlkem1024.NewKeyFromSeed(standard.Bytes())
	encodedPublic, err := public.MarshalBinary()
	if err != nil || !bytes.Equal(encodedPublic, standard.EncapsulationKey().Bytes()) {
		t.Fatal("ML-KEM public encoding differs across implementations")
	}
	encodedPrivate, err := private.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	shared, ciphertext := standard.EncapsulationKey().Encapsulate()
	actual, err := wire.DecapsulateSignalPQ(encodedPrivate, ciphertext)
	if err != nil || !bytes.Equal(actual, shared) {
		t.Fatal("native expanded key cannot decapsulate standard-library ciphertext")
	}
	if _, err := wire.DecapsulateSignalPQ(encodedPrivate, ciphertext[:len(ciphertext)-1]); err == nil {
		t.Fatal("truncated PQ ciphertext was accepted")
	}
}

func TestSignalPQPreKeyRejectsMismatchedAndUnsignedMaterial(t *testing.T) {
	identity, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := wire.NewSignalPQPreKey(identity.Bytes(), 17)
	if err != nil || wire.ValidateSignalPQPreKey(key, identity.PublicKey().Bytes()) != nil {
		t.Fatal("generated PQ prekey is invalid")
	}
	key.Private[0] ^= 1
	if wire.ValidateSignalPQPreKey(key, identity.PublicKey().Bytes()) == nil {
		t.Fatal("PQ private key corruption was accepted")
	}
	key.Private[0] ^= 1
	key.Signature[0] ^= 1
	if wire.ValidateSignalPQPreKey(key, identity.PublicKey().Bytes()) == nil {
		t.Fatal("PQ signature corruption was accepted")
	}
}

package wire

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestSealRegistrationCryptoKitVector(t *testing.T) {
	// RFC 7748 example keys; expected frame independently produced by CryptoKit.
	privateBytes, err := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		t.Fatal(err)
	}
	serverBytes, err := hex.DecodeString("de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ecdh.X25519().NewPublicKey(serverBytes)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := sealRegistration([]byte("field=a%2Bb&t=1700000000"), server, private)
	if err != nil {
		t.Fatal(err)
	}
	const want = "hSDwCYkwp1R0i33ctD73Wg2_Og0mOBr066SpjqqbTmq2wPp3YRG1bfmEktrxygvbk5JMZfsdnamp9uai6uLhEyP2BwxMYzNC"
	got := base64.RawURLEncoding.EncodeToString(frame)
	if got != want {
		t.Fatalf("registration frame = %q, want %q", got, want)
	}
}

func TestSealRegistrationReceiver(t *testing.T) {
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("synthetic registration fields")
	var previous []byte
	for range 2 {
		frame, err := SealRegistration(plaintext, server.PublicKey().Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if len(frame) != 32+len(plaintext)+16 {
			t.Fatalf("frame length = %d, want %d", len(frame), 32+len(plaintext)+16)
		}
		if bytes.Equal(frame[:32], previous) {
			t.Fatal("registration envelope reused the ephemeral public key")
		}
		previous = bytes.Clone(frame[:32])
		client, err := ecdh.X25519().NewPublicKey(frame[:32])
		if err != nil {
			t.Fatal(err)
		}
		shared, err := server.ECDH(client)
		if err != nil {
			t.Fatal(err)
		}
		block, err := aes.NewCipher(shared)
		if err != nil {
			t.Fatal(err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
		got, err := aead.Open(nil, make([]byte, 12), frame[32:], nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("decrypted registration = %q, want %q", got, plaintext)
		}
	}
}

func TestSealRegistrationRejectsInvalidServerKey(t *testing.T) {
	for _, key := range [][]byte{nil, make([]byte, 31), make([]byte, 32)} {
		if _, err := SealRegistration([]byte("synthetic"), key); err == nil {
			t.Fatalf("accepted invalid server key of length %d", len(key))
		}
	}
}

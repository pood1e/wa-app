package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestPasswordEnvelopePreservesText(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const password = " Password1 "
	sealed, err := encryptPassword(password, &key.PublicKey, 7, 1800000000)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := protocoltest.OpenPassword(key, sealed)
	if err != nil || plain != password {
		t.Fatal("server could not decrypt the exact password with native field ordering")
	}
	if _, err = encryptPassword(password, &key.PublicKey, 256, 1800000000); err == nil {
		t.Fatal("invalid server key ID was accepted")
	}
}

func TestCanonicalEncryptionUsesCertificateParameters(t *testing.T) {
	for _, bits := range []int{2048, 4096} {
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			t.Fatal(err)
		}
		value, err := sealCanonical([]byte("synthetic-canonical-payload"), &key.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		wrapped, _ := base64.StdEncoding.DecodeString(value.Key)
		hash := sha1.New()
		nonceSize := 12
		var aad []byte
		if bits == 4096 {
			hash = sha256.New()
			nonceSize = 16
			aad = x509.MarshalPKCS1PublicKey(&key.PublicKey)
		}
		secret, err := rsa.DecryptOAEP(hash, rand.Reader, key, wrapped, nil)
		if err != nil {
			t.Fatal("canonical RSA parameters differ from the native certificate branch")
		}
		block, err := aes.NewCipher(secret)
		if err != nil {
			t.Fatal(err)
		}
		gcm, err := cipher.NewGCMWithNonceSize(block, nonceSize)
		if err != nil {
			t.Fatal(err)
		}
		nonce, _ := base64.StdEncoding.DecodeString(value.Nonce)
		data, _ := base64.StdEncoding.DecodeString(value.Data)
		tag, _ := base64.StdEncoding.DecodeString(value.Tag)
		plain, err := gcm.Open(nil, nonce, append(data, tag...), aad)
		if err != nil || string(plain) != "synthetic-canonical-payload" {
			t.Fatal("canonical AEAD parameters differ from the native certificate branch")
		}
	}
}

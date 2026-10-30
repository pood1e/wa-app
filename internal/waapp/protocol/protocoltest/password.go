package protocoltest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"strings"
	"testing"
)

func OpenPassword(private *rsa.PrivateKey, value string) (string, error) {
	parts := strings.SplitN(value, ":", 4)
	if len(parts) != 4 || parts[0] != "#PWD_WA" || parts[1] != "11" {
		return "", errors.New("invalid password envelope")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(raw) < 32 {
		return "", errors.New("invalid password ciphertext")
	}
	n := int(binary.LittleEndian.Uint16(raw[14:16]))
	if raw[0] != 1 || raw[1] != 7 || n != private.Size() || len(raw) < 16+n+16 {
		return "", errors.New("invalid password field order")
	}
	key, err := rsa.DecryptPKCS1v15(rand.Reader, private, raw[16:16+n])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := append(append([]byte(nil), raw[32+n:]...), raw[16+n:32+n]...)
	plain, err := gcm.Open(nil, raw[2:14], sealed, nil)
	return string(plain), err
}

func PasswordKey(t testing.TB) (*protocolv1.PasswordEncryptionKey, *rsa.PrivateKey) {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &protocolv1.PasswordEncryptionKey{PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), KeyId: 7}, private
}

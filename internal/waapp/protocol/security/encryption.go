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
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
)

type encryptedValue struct {
	Algorithm string `json:"algorithm"`
	Data      string `json:"data"`
	Key       string `json:"key"`
	Nonce     string `json:"nonce"`
	Tag       string `json:"tag"`
}

func parsePublicKey(text string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("account security public key is invalid")
	}
	var key any
	var err error
	switch block.Type {
	case "CERTIFICATE":
		var cert *x509.Certificate
		cert, err = x509.ParseCertificate(block.Bytes)
		if err == nil {
			key = cert.PublicKey
		}
	case "PUBLIC KEY":
		key, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		key, err = x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		err = errors.New("unsupported public key")
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if err != nil || !ok || rsaKey.N.BitLen() < 2048 {
		return nil, errors.New("account security RSA public key is invalid")
	}
	return rsaKey, nil
}

func encryptPassword(password string, key *rsa.PublicKey, keyID int, timestamp int64) (string, error) {
	if keyID < 1 || keyID > 255 {
		return "", errors.New("account password key ID is invalid")
	}
	symmetric := make([]byte, 32)
	if _, err := rand.Read(symmetric); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(symmetric)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(password), nil)
	wrapped, err := rsa.EncryptPKCS1v15(rand.Reader, key, symmetric)
	if err != nil {
		return "", err
	}
	if len(wrapped) > 65535 {
		return "", errors.New("account password encryption key is too large")
	}
	data := make([]byte, 0, 16+len(wrapped)+len(sealed))
	data = append(data, 1, byte(keyID))
	data = append(data, nonce...)
	data = binary.LittleEndian.AppendUint16(data, uint16(len(wrapped)))
	data = append(data, wrapped...)
	data = append(data, sealed[len(sealed)-gcm.Overhead():]...)
	data = append(data, sealed[:len(sealed)-gcm.Overhead()]...)
	return fmt.Sprintf("#PWD_WA:11:%d:%s", timestamp, base64.StdEncoding.EncodeToString(data)), nil
}

func sealCanonical(data []byte, server *rsa.PublicKey) (*encryptedValue, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	var aad []byte
	hash := sha1.New()
	if server.N.BitLen() >= 4096 {
		nonce = make([]byte, 16)
		aad = x509.MarshalPKCS1PublicKey(server)
		hash = sha256.New()
	} else if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, data, aad)
	wrapped, err := rsa.EncryptOAEP(hash, rand.Reader, server, key, nil)
	if err != nil {
		return nil, err
	}
	return &encryptedValue{Algorithm: "rsa2048", Data: base64.StdEncoding.EncodeToString(sealed[:len(sealed)-gcm.Overhead()]), Key: base64.StdEncoding.EncodeToString(wrapped), Nonce: base64.StdEncoding.EncodeToString(nonce), Tag: base64.StdEncoding.EncodeToString(sealed[len(sealed)-gcm.Overhead():])}, nil
}

func openCanonical(value *encryptedValue, client *rsa.PrivateKey) ([]byte, error) {
	if value == nil || value.Algorithm != "rsa2048" {
		return nil, errors.New("canonical credential response is invalid")
	}
	wrapped, err := base64.StdEncoding.DecodeString(value.Key)
	if err != nil {
		return nil, errors.New("canonical credential key is invalid")
	}
	key, err := rsa.DecryptOAEP(sha1.New(), rand.Reader, client, wrapped, nil)
	if err != nil {
		return nil, errors.New("canonical credential key could not be decrypted")
	}
	data, err := base64.StdEncoding.DecodeString(value.Data)
	if err != nil {
		return nil, errors.New("canonical credential data is invalid")
	}
	tag, err := base64.StdEncoding.DecodeString(value.Tag)
	if err != nil || len(tag) != 16 {
		return nil, errors.New("canonical credential tag is invalid")
	}
	nonce, err := base64.StdEncoding.DecodeString(value.Nonce)
	if err != nil || (len(nonce) != 12 && len(nonce) != 16) {
		return nil, errors.New("canonical credential nonce is invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("canonical credential AES key is invalid")
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, append(data, tag...), nil)
	if err != nil {
		return nil, errors.New("canonical credential authentication failed")
	}
	return plain, nil
}

func newCredentialPassword() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()_+{}"
	result := make([]byte, 50)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result[i] = alphabet[n.Int64()]
	}
	return string(result), nil
}

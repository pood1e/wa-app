package security

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestDevicePairingMetadataUsesTheNativeNestedCredentialEnvelope(t *testing.T) {
	encryption, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	password, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := func(key *rsa.PublicKey) string {
		der, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	}
	encryptionPEM, passwordPEM := public(&encryption.PublicKey), public(&password.PublicKey)
	now := time.Unix(1800000000, 0)
	client := New(func(_ context.Context, op Operation, _ any) (json.RawMessage, error) {
		if op != GetCertificates {
			t.Fatal("pairing changed the account credentials instead of sealing their proof")
		}
		return json.Marshal(struct {
			Certificates *canonicalCertificates `json:"xwa2_ent_get_certificates"`
		}{&canonicalCertificates{Encryption: &canonicalCertificate{PEM: encryptionPEM}, Password: &canonicalCertificate{PEM: passwordPEM, ID: 7}}})
	}, nil, nil, func() time.Time { return now }, nil)
	credentials := &protocolv1.CanonicalCredentials{UserId: "9001", CredentialPassword: "synthetic-credential-password"}
	metadata, err := client.PairingMetadata(t.Context(), credentials)
	if err != nil || metadata.Tag != "encryption-metadata" || metadata.Attrs["version"] != "1" || metadata.Attrs["algorithm"] != "rsa2048" {
		t.Fatal("pairing omitted the native encryption envelope", err)
	}
	fields := map[string]string{}
	for _, child := range wire.ChatdChildren(metadata) {
		raw, ok := child.Content.([]byte)
		if !ok || bytes.Contains(raw, []byte(credentials.GetCredentialPassword())) {
			t.Fatal("pairing exposed an unsealed credential")
		}
		fields[child.Tag] = base64.StdEncoding.EncodeToString(raw)
	}
	plain, err := openCanonical(&encryptedValue{Algorithm: "rsa2048", Key: fields["encrypted_key"], Nonce: fields["nonce"], Data: fields["encrypted_data"], Tag: fields["auth_tag"]}, encryption)
	if err != nil {
		t.Fatal("server could not authenticate the pairing proof", err)
	}
	var proof struct {
		Version   int    `json:"version"`
		Timestamp int64  `json:"timestamp"`
		Password  string `json:"password"`
		PublicKey string `json:"client_pub_key"`
		KeyType   string `json:"client_pub_key_type"`
	}
	if json.Unmarshal(plain, &proof) != nil || proof.Version != 1 || proof.Timestamp != now.Unix() || proof.PublicKey != passwordPEM || proof.KeyType != "RSA 2048" {
		t.Fatal("pairing substituted the server password key or native envelope fields")
	}
	decoded, err := protocoltest.OpenPassword(password, proof.Password)
	if err != nil || decoded != credentials.GetCredentialPassword() {
		t.Fatal("server could not recover the original generated credential secret")
	}
	if _, err := client.PairingMetadata(t.Context(), nil); err == nil {
		t.Fatal("pairing accepted missing primary authorization material")
	}
}

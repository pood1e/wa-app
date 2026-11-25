package communication

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"sync"
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

var pairingCertificate = sync.OnceValues(func() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
})

func pairingTestConnection(t *testing.T, current state.NativeState) (*Connection, *ChatdTransport) {
	t.Helper()
	current.Canonical = &protocolv1.CanonicalCredentials{UserId: "9001", AccessToken: "synthetic-access-token", CredentialPassword: "synthetic-canonical-password"}
	connection, peer, _ := groupTestConnection(t, current)
	return connection, peer
}

func pairingTestIQ(t *testing.T, peer *ChatdTransport) wire.ChatdNode {
	t.Helper()
	request := deviceTestIQ(t, peer, "w:mex")
	query, _ := ChatdChild(request, "query")
	if query.Attrs["query_id"] != security.GetCertificates.DocumentID() {
		t.Fatal("pairing did not request authenticated server certificates")
	}
	public, err := pairingCertificate()
	if err != nil {
		t.Fatal(err)
	}
	certificate := struct {
		PEM string `json:"pem"`
		ID  int    `json:"key_id"`
	}{public, 7}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"xwa2_ent_get_certificates": map[string]any{"encryption_pem": certificate, "password_pem": certificate}}})
	if err != nil {
		t.Fatal(err)
	}
	deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "result", Attrs: map[string]string{"format": "json"}, Content: raw})
	return deviceTestIQ(t, peer, "md")
}

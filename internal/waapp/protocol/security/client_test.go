package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestPasswordSettingReadsBackTwoStepVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
	for _, change := range []bool{false, true} {
		for _, enable := range []bool{false, true} {
			enabled := false
			writes, reads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Document  int64  `json:"doc_id"`
					Token     string `json:"access_token"`
					Variables string `json:"variables"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.Token != "synthetic-access" {
					t.Error("canonical HTTP authorization was lost")
					return
				}
				if request.Document == 27318258844468227 {
					_ = json.NewEncoder(w).Encode(struct {
						Data any `json:"data"`
					}{struct {
						Key any `json:"wa_password_encryption_key"`
					}{struct {
						PEM string `json:"public_key"`
						ID  int    `json:"key_id"`
					}{public, 7}}})
					return
				}
				var input struct {
					Data struct {
						Password string `json:"encrypted_password"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(request.Variables), &input) != nil {
					t.Error("password variables were malformed")
					return
				}
				plain, err := protocoltest.OpenPassword(key, input.Data.Password)
				if err != nil || plain != " Password1 " {
					t.Error("exact user password did not reach the server encrypted")
					return
				}
				writes++
				field := "xfb_wa_create_password"
				document := int64(36595298760118580)
				if change {
					field = "xfb_wa_change_password"
					document = 27534818572826036
				}
				if request.Document != document {
					t.Error("create/change selection ignored the remote password state")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{field: map[string]bool{"success": true}}})
			}))
			mex := func(_ context.Context, op Operation, _ any) (json.RawMessage, error) {
				switch op {
				case HasPassword:
					b, _ := json.Marshal(struct {
						Status any `json:"xwa2_has_password"`
					}{struct {
						Has bool `json:"has_password"`
					}{change}})
					return b, nil
				case GetTwofa:
					reads++
					b, _ := json.Marshal(struct {
						Status any `json:"xwa2_get_twofa"`
					}{struct {
						Enabled bool `json:"twofa_enabled"`
					}{enabled}})
					return b, nil
				case SetTwofa:
					enabled = enable
					return []byte(`{"xwa2_set_twofa":{"success":true}}`), nil
				default:
					return nil, errors.New("unexpected operation")
				}
			}
			client := New(mex, server.Client(), nil, time.Now, nil)
			client.endpoint = server.URL
			status, err := client.SetPassword(t.Context(), " Password1 ", &protocolv1.CanonicalCredentials{AccessToken: "synthetic-access"})
			server.Close()
			if writes != 1 || reads != 2 || (err == nil) != enable {
				t.Fatal("password was retried or local success ignored the server readback")
			}
			if enable && (status == nil || !status.Configured) {
				t.Fatal("confirmed two-step verification state was lost")
			}
		}
	}
}

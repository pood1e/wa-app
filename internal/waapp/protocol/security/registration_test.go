package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"google.golang.org/protobuf/proto"
)

func TestRegistrationPasswordKeyNeedsNoAccountAuthorization(t *testing.T) {
	key, _ := protocoltest.PasswordKey(t)
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		t.Run(platform.String(), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var input struct {
					Document  int64  `json:"doc_id"`
					Token     string `json:"access_token"`
					Variables string `json:"variables"`
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Document != 27318258844468227 || !strings.HasPrefix(input.Token, "WA|") || input.Variables != "{}" {
					t.Error("public-key query did not use native application authorization")
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("public-key query inherited account authorization")
				}
				_ = json.NewEncoder(w).Encode(struct {
					Data any `json:"data"`
				}{struct {
					Key *protocolv1.PasswordEncryptionKey `json:"wa_password_encryption_key"`
				}{key}})
			}))
			t.Cleanup(server.Close)
			client := New(nil, server.Client(), &protocolv1.ClientMetadata{Platform: platform}, time.Now, nil)
			client.endpoint = server.URL
			resolved, err := client.RegistrationPasswordEncryptionKey(t.Context())
			if err != nil || !proto.Equal(resolved, key) || calls != 1 {
				t.Fatal("first-time account password key lookup failed")
			}
		})
	}
}

package appeal

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
)

// Submit fetches fresh server keys and sends one encrypted review request.
// Callers query Status before retrying an ambiguous submission.
func (c *Client) Submit(ctx context.Context, token, description string, metadata *protocolv1.ClientMetadata) (protocolv1.AccountReviewStatus, error) {
	unknown := protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return unknown, err
	}
	variables := struct {
		AppID  string `json:"app_id"`
		Public string `json:"client_pub_key"`
	}{applicationID, base64.RawStdEncoding.EncodeToString(private.PublicKey().Bytes())}
	data, err := c.post(ctx, keysDocument, variables, metadata)
	if err != nil {
		return unknown, err
	}
	if data.Keys == nil {
		return unknown, errors.New("account review response is missing its encryption keys")
	}
	debug := struct {
		Context string `json:"Context"`
		Origin  string `json:"ban_appeal_ui_origin"`
		Agent   string `json:"useragent"`
		Client  string `json:"Client implementation"`
	}{"blocked_ban_appeals", "logged_out", graph.UserAgent(metadata), "wa-app Go client"}
	rawDebug, err := json.Marshal(debug)
	if err != nil {
		return unknown, err
	}
	request := struct {
		Token       string `json:"token"`
		DebugInfo   string `json:"debug_info"`
		Description string `json:"description"`
	}{token, string(rawDebug), description}
	plaintext, err := json.Marshal(request)
	if err != nil {
		return unknown, err
	}
	encrypted, err := encrypt(private, *data.Keys, plaintext)
	if err != nil {
		return unknown, err
	}
	submission := struct {
		AppID   string            `json:"app_id"`
		Request *encryptedRequest `json:"user_request"`
	}{applicationID, encrypted}
	result, err := c.post(ctx, submitDocument, submission, metadata)
	if err != nil {
		return unknown, reviewError("account review submission result is unknown; refresh its status before submitting again", false)
	}
	return parseStatus(result.Submitted)
}

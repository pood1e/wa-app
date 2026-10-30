package security

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
)

type Exchange func(context.Context, Operation, any) (json.RawMessage, error)
type NonceReceiver func(context.Context) (string, error)

type Client struct {
	mex      Exchange
	http     *http.Client
	metadata *protocolv1.ClientMetadata
	now      func() time.Time
	endpoint string
	nonce    NonceReceiver
}

func New(mex Exchange, client *http.Client, metadata *protocolv1.ClientMetadata, now func() time.Time, nonce NonceReceiver) *Client {
	return &Client{mex: mex, http: client, metadata: metadata, now: now, endpoint: graph.Endpoint, nonce: nonce}
}

func variables(input any) any {
	return struct {
		Input any `json:"input"`
	}{input}
}

func query[T any](ctx context.Context, c *Client, op Operation, variables any) (*T, error) {
	data, err := c.mex(ctx, op, variables)
	if err != nil {
		return nil, err
	}
	var result T
	if json.Unmarshal(data, &result) != nil {
		return nil, errors.New("account security response is invalid")
	}
	return &result, nil
}

func (c *Client) Status(ctx context.Context) (*waappv1.TwoFactorAuthStatus, error) {
	result, err := query[struct {
		Status *struct {
			Enabled *bool `json:"twofa_enabled"`
			Methods []*struct {
				Method string `json:"method"`
				Value  string `json:"value"`
				State  string `json:"state"`
			} `json:"twofa_methods"`
		} `json:"xwa2_get_twofa"`
	}](ctx, c, GetTwofa, variables(struct{}{}))
	if err != nil {
		return nil, err
	}
	if result.Status == nil || result.Status.Enabled == nil {
		return nil, errors.New("two-step verification status is missing")
	}
	status := &waappv1.TwoFactorAuthStatus{Configured: *result.Status.Enabled}
	for _, method := range result.Status.Methods {
		if method != nil && method.Method == "email" && method.Value != "" {
			status.EmailAddress = method.Value
			status.EmailConfigured = true
			status.EmailVerified = method.State == "verified"
		}
	}
	return status, nil
}

func (c *Client) SetPassword(ctx context.Context, password string, credentials *protocolv1.CanonicalCredentials) (*waappv1.TwoFactorAuthStatus, error) {
	has, err := query[struct {
		Status *struct {
			Has *bool `json:"has_password"`
		} `json:"xwa2_has_password"`
	}](ctx, c, HasPassword, struct{}{})
	if err != nil {
		return nil, err
	}
	if has.Status == nil || has.Status.Has == nil {
		return nil, errors.New("account password status is missing")
	}
	sealed, err := c.EncryptPassword(ctx, password, credentials.GetAccessToken())
	if err != nil {
		return nil, err
	}
	input := struct {
		Data struct {
			Password string `json:"encrypted_password"`
		} `json:"data"`
	}{}
	input.Data.Password = sealed
	document := int64(36595298760118580)
	if *has.Status.Has {
		document = 27534818572826036
	}
	data, err := c.post(ctx, document, input, credentials.GetAccessToken())
	if err != nil {
		return nil, err
	}
	type outcome struct {
		Success *bool `json:"success"`
	}
	var result struct {
		Created *outcome `json:"xfb_wa_create_password"`
		Changed *outcome `json:"xfb_wa_change_password"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, errors.New("account password result is invalid")
	}
	accepted := result.Created
	if *has.Status.Has {
		accepted = result.Changed
	}
	if accepted == nil || accepted.Success == nil || !*accepted.Success {
		return nil, errors.New("account password was not accepted")
	}
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	if status.Configured {
		return status, nil
	}
	changed, err := query[struct {
		Result *struct {
			Success *bool `json:"success"`
			Already bool  `json:"twofa_already_enabled"`
		} `json:"xwa2_set_twofa"`
	}](ctx, c, SetTwofa, variables(struct {
		Enabled bool `json:"twofa_enabled"`
	}{true}))
	if err != nil {
		return nil, err
	}
	if changed.Result == nil || (changed.Result.Success == nil && !changed.Result.Already) || (changed.Result.Success != nil && !*changed.Result.Success && !changed.Result.Already) {
		return nil, errors.New("password was saved but two-step verification was not enabled")
	}
	status, err = c.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !status.Configured {
		return nil, errors.New("password was saved but two-step verification remains disabled")
	}
	return status, nil
}

func (c *Client) post(ctx context.Context, document int64, variables any, token string) (json.RawMessage, error) {
	return graph.Post(ctx, c.http, c.endpoint, graph.Request{DocumentID: document, Variables: variables, AccessToken: token, Metadata: c.metadata})
}

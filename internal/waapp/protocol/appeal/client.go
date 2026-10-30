// Package appeal implements account review without a device signing worker.
package appeal

import (
	"context"
	_ "embed"
	"errors"
	"net/http"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

const (
	statusDocument int64 = 0xf3123d2188cf7
	keysDocument   int64 = 0x149e09feba5e1b
	submitDocument int64 = 0x1371f3dd6320f6
	applicationID        = "dev.app.id"
)

//go:embed graphql-2.26.40.76.pb
var applicationData []byte

// Client owns the GraphQL transport for review queries and encrypted submissions.
type Client struct {
	http        *http.Client
	endpoint    string
	application *protocolv1.GraphQLApplicationMaterial
}

// New shares the supplied transport and disables redirects for authenticated requests.
func New(client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("account review HTTP client is required")
	}
	material := new(protocolv1.GraphQLApplicationMaterial)
	if err := proto.Unmarshal(applicationData, material); err != nil || material.GetAccessToken() == "" {
		return nil, errors.New("invalid bundled GraphQL application material")
	}
	owned := *client
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &owned, endpoint: "https://graph.whatsapp.com/graphql", application: material}, nil
}

// Status queries the decision associated with an account's private review token.
func (c *Client) Status(ctx context.Context, token string, metadata *protocolv1.ClientMetadata) (protocolv1.AccountReviewStatus, error) {
	variables := struct {
		AppID string `json:"app_id"`
		Token string `json:"request_token"`
	}{applicationID, token}
	data, err := c.post(ctx, statusDocument, variables, metadata)
	if err != nil {
		return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_UNSPECIFIED, err
	}
	return parseStatus(data.Status)
}

func parseStatus(value *wireStatus) (protocolv1.AccountReviewStatus, error) {
	if value != nil {
		switch value.Status {
		case "NO_APPEAL_OPENED":
			return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_NOT_REQUESTED, nil
		case "IN_REVIEW":
			return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW, nil
		case "BANNED":
			return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_REJECTED, nil
		case "UNBANNED":
			return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_APPROVED, nil
		}
	}
	return protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_UNSPECIFIED, errors.New("invalid account review status response")
}

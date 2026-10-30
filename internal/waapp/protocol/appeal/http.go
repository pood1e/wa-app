package appeal

import (
	"context"
	"encoding/json"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type wireStatus struct {
	Status string `json:"status"`
}

type responseData struct {
	Status    *wireStatus `json:"whatsapp_support_ban_appeal_status"`
	Submitted *wireStatus `json:"whatsapp_support_process_ban_appeal_request_encrypted"`
	Keys      *serverKeys `json:"purpose_public_keys"`
}

func (c *Client) post(ctx context.Context, document int64, variables any, metadata *protocolv1.ClientMetadata) (*responseData, error) {
	data, err := graph.Post(ctx, c.http, c.endpoint, graph.Request{DocumentID: document, Variables: variables, AccessToken: c.application.GetAccessToken(), Metadata: metadata})
	if err != nil {
		return nil, reviewError(err.Error(), graph.Retryable(err))
	}
	var result responseData
	if json.Unmarshal(data, &result) != nil {
		return nil, reviewError("invalid account review response", false)
	}
	return &result, nil
}

func reviewError(message string, retryable bool) error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, retryable)
}

package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

const Endpoint = "https://graph.whatsapp.com/graphql"

type Request struct {
	DocumentID  int64
	Variables   any
	AccessToken string
	Metadata    *protocolv1.ClientMetadata
}

func Post(ctx context.Context, client *http.Client, endpoint string, input Request) (json.RawMessage, error) {
	if client == nil {
		return nil, errors.New("GraphQL HTTP client is unavailable")
	}
	variables, err := json.Marshal(input.Variables)
	if err != nil {
		return nil, err
	}
	payload := struct {
		Variables   string `json:"variables"`
		AccessToken string `json:"access_token"`
		DocumentID  int64  `json:"doc_id"`
		Language    string `json:"lang"`
		ContentType string `json:"Content-Type"`
	}{string(variables), input.AccessToken, input.DocumentID, "en_US", "application/json"}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", UserAgent(input.Metadata))
	owned := *client
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := owned.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, UpstreamError{}
	}
	defer response.Body.Close()
	const maxResponse = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return nil, UpstreamError{}
	}
	if len(raw) > maxResponse {
		return nil, errors.New("GraphQL response could not be read")
	}
	if response.StatusCode != http.StatusOK {
		return nil, UpstreamError{Status: response.StatusCode}
	}
	return Decode(raw, "json", nil)
}

func UserAgent(metadata *protocolv1.ClientMetadata) string {
	version := metadata.GetAppVersion()
	if !strings.HasPrefix(version, "2.") {
		version = "2." + version
	}
	if metadata.GetPlatform() == protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS {
		return "WhatsApp/" + version + " iOS/" + metadata.GetOsVersion()
	}
	return "WhatsApp/" + version + " Android/" + metadata.GetOsVersion() + " Device/" + metadata.GetManufacturer() + "-" + metadata.GetDevice()
}

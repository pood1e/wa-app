package ios

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

const (
	registrationBaseURL     = "https://v.whatsapp.net/v2/"
	maxRegistrationResponse = 64 * 1024
)

type registrationHTTP struct {
	client      *http.Client
	baseURL     string
	serverKey   []byte
	userAgent   string
	clockOffset time.Duration
}

// HTTPStatusError reports an upstream HTTP status without retaining its body.
type HTTPStatusError struct{ StatusCode int }

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("registration HTTP status %d", e.StatusCode)
}

func newRegistrationHTTP(client *http.Client, userAgent string) *registrationHTTP {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	owned := *client
	if owned.Timeout <= 0 || owned.Timeout > 30*time.Second {
		owned.Timeout = 30 * time.Second
	}
	owned.Jar = nil
	owned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	key, _ := hex.DecodeString("8e8c0f74c3ebc5d7a6865c6c3c843856b06121cce8ea774d22fb6f122512302d")
	return &registrationHTTP{client: &owned, baseURL: registrationBaseURL, serverKey: key, userAgent: userAgent}
}

func (c *registrationHTTP) prepare(ctx context.Context, path string, query parameters, attestor *Attestor, authorization string) (*http.Request, error) {
	frame, err := wire.SealRegistration(query.plaintext(), c.serverKey)
	if err != nil {
		return nil, err
	}
	body, err := attestor.RegistrationBody(ctx, frame)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", c.userAgent)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	return request, nil
}

func (c *registrationHTTP) send(request *http.Request) (*protocolv1.IOSRegistrationResponse, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send iOS registration request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		wire.LogRegistrationResponse(request.URL, response.StatusCode, nil, "")
		return nil, &HTTPStatusError{StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRegistrationResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read iOS registration response: %w", err)
	}
	if len(body) > maxRegistrationResponse {
		return nil, errors.New("iOS registration response exceeds size limit")
	}
	wire.LogRegistrationResponse(request.URL, response.StatusCode, body, "")
	result := new(protocolv1.IOSRegistrationResponse)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, result); err != nil {
		return nil, errors.New("iOS registration response is invalid")
	}
	if result.GetStatus() == "" {
		return nil, errors.New("iOS registration response status is missing")
	}
	if serverDate, err := http.ParseTime(response.Header.Get("Date")); err == nil {
		c.clockOffset = time.Until(serverDate)
	}
	return result, nil
}

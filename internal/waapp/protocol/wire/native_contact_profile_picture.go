package wire

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const (
	DefaultContactProfilePictureTimeout = 20 * time.Second
	ProfilePictureDirectPathHost        = "https://pps.whatsapp.net"
	ProfilePictureDownloadMaxBytes      = 2 << 20
	ProfilePictureTypeImage             = "image"
	ProfilePictureTypePreview           = "preview"
)

type ContactProfilePictureLocation struct {
	ID         string
	DirectPath string
	URL        string
	InlineData []byte
}

func (c *NativeHTTPClient) GetProfilePicture(ctx context.Context, location ContactProfilePictureLocation, userAgent string) ([]byte, string, error) {
	endpoints := ProfilePictureDownloadURLs(location)
	var lastErr error
	for _, endpoint := range endpoints {
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				if err := SleepWithContext(ctx, 250*time.Millisecond); err != nil {
					return nil, "", err
				}
			}
			data, contentType, err := c.GetProfilePictureOnce(ctx, endpoint, userAgent)
			if err == nil {
				return data, contentType, nil
			}
			lastErr = err
			if !ProfilePictureDownloadRetryable(err) {
				break
			}
		}
	}
	if lastErr == nil {
		lastErr = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	return nil, "", lastErr
}

func (c *NativeHTTPClient) GetProfilePictureOnce(ctx context.Context, endpoint string, userAgent string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", shared.FirstNonEmpty(userAgent, "WhatsApp/"+DefaultWAAppVersion+" Android/13"))
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := ReadLimitedProfilePicture(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", ProfilePictureHTTPError(resp.StatusCode)
	}
	contentType, err := ProfilePictureContentType(data, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func ProfilePictureDownloadURLs(location ContactProfilePictureLocation) []string {
	out := []string{}
	seen := map[string]struct{}{}
	appendEndpoint := func(endpoint string) {
		if endpoint == "" {
			return
		}
		if _, ok := seen[endpoint]; ok {
			return
		}
		seen[endpoint] = struct{}{}
		out = append(out, endpoint)
	}
	if endpoint, ok := NormalizeProfilePictureURL(location.URL); ok {
		appendEndpoint(endpoint)
	}
	directPath := strings.TrimSpace(location.DirectPath)
	if ProfilePictureDirectPathSigned(directPath) {
		if endpoint, ok := NormalizeProfilePictureURL(directPath); ok {
			appendEndpoint(endpoint)
		}
	}
	return out
}

func NormalizeProfilePictureURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "/") {
		return ProfilePictureDirectPathHost + value, true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return "", false
	}
	if parsed.Scheme == "" && strings.HasPrefix(parsed.Path, "/") {
		return ProfilePictureDirectPathHost + parsed.String(), true
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || !ProfilePictureURLHostAllowed(parsed.Host) {
		return "", false
	}
	return parsed.String(), true
}

func ProfilePictureDirectPathSigned(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return false
	}
	query := parsed.Query()
	return query.Get("oe") != "" && query.Get("oh") != ""
}

func ProfilePictureURLHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if cut, _, ok := strings.Cut(host, ":"); ok {
		host = cut
	}
	return host == "whatsapp.net" || strings.HasSuffix(host, ".whatsapp.net") ||
		host == "fbcdn.net" || strings.HasSuffix(host, ".fbcdn.net") ||
		host == "fbsbx.com" || strings.HasSuffix(host, ".fbsbx.com")
}

func ReadLimitedProfilePicture(reader io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	limited := io.LimitReader(reader, ProfilePictureDownloadMaxBytes+1)
	if _, err := buf.ReadFrom(limited); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	if len(data) > ProfilePictureDownloadMaxBytes {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA profile picture is too large", false)
	}
	return data, nil
}

func ProfilePictureContentType(data []byte, header string) (string, error) {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if ProfilePictureContentTypeAllowed(contentType) {
		return contentType, nil
	}
	detected := strings.ToLower(http.DetectContentType(data))
	if ProfilePictureContentTypeAllowed(detected) {
		return detected, nil
	}
	return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA profile picture content type is not supported", false)
}

func ProfilePictureContentTypeAllowed(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func ProfilePictureHTTPError(statusCode int) error {
	retryable := statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, fmt.Sprintf("WA profile picture download failed with HTTP %d", statusCode), retryable)
}

func ProfilePictureDownloadRetryable(err error) bool {
	appErr, ok := err.(*shared.AppError)
	if !ok {
		return true
	}
	return appErr.Retryable
}

func SleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package communication

import (
	"context"
	"errors"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ContactProfilePictureTargetKind(jid string) string {
	jid = wacore.NormalizeWAJID(jid)
	switch {
	case strings.HasSuffix(jid, "@lid"):
		return "lid"
	case strings.HasSuffix(jid, "@s.whatsapp.net"):
		return "pn"
	case jid == "":
		return "empty"
	default:
		return "other"
	}
}

func ContactProfilePictureRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || shared.DigitsOnly(value) != value || strings.TrimLeft(value, "0") == "" {
		return ""
	}
	return value
}

func ContactProfilePictureLocationFromIQ(response pwire.ChatdNode) (pwire.ContactProfilePictureLocation, error) {
	if response.Attrs["type"] == "error" {
		return pwire.ContactProfilePictureLocation{}, ContactProfilePictureIQError(response)
	}
	picture, ok := FindChatdNode(response, "picture")
	if !ok {
		return pwire.ContactProfilePictureLocation{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	return ContactProfilePictureLocationFromPicture(picture)
}

func ContactProfilePictureIQError(response pwire.ChatdNode) error {
	code := strings.TrimSpace(response.Attrs["code"])
	if errorNode, ok := FindChatdNode(response, "error"); ok {
		code = shared.FirstNonEmpty(code, strings.TrimSpace(errorNode.Attrs["code"]))
	}
	if code == "404" || code == "410" {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA profile picture request was rejected", false)
}

func ContactProfilePictureLocationFromPicture(picture pwire.ChatdNode) (pwire.ContactProfilePictureLocation, error) {
	status := strings.TrimSpace(picture.Attrs["status"])
	if status != "" && status != "200" && status != "ok" {
		return pwire.ContactProfilePictureLocation{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	location := pwire.ContactProfilePictureLocation{
		ID:         ContactProfilePictureIDFromPicture(picture),
		DirectPath: strings.TrimSpace(picture.Attrs["direct_path"]),
		URL:        strings.TrimSpace(picture.Attrs["url"]),
	}
	if data, ok := picture.Content.([]byte); ok && len(data) > 0 {
		location.InlineData = data
	}
	if len(location.InlineData) == 0 && location.DirectPath == "" && location.URL == "" {
		return pwire.ContactProfilePictureLocation{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	return location, nil
}

func ContactProfilePictureIDFromPicture(picture pwire.ChatdNode) string {
	return strings.TrimSpace(shared.FirstNonEmpty(picture.Attrs["id"], picture.Attrs["photo_id"], picture.Attrs["picture_id"]))
}

func ContactProfilePictureLocationDownloadable(location pwire.ContactProfilePictureLocation) bool {
	if len(location.InlineData) > 0 {
		return true
	}
	if _, ok := pwire.NormalizeProfilePictureURL(location.URL); ok {
		return true
	}
	directPath := strings.TrimSpace(location.DirectPath)
	if !pwire.ProfilePictureDirectPathSigned(directPath) {
		return false
	}
	_, ok := pwire.NormalizeProfilePictureURL(directPath)
	return ok
}

func ContactProfilePictureFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "username/password authentication failed"):
		return "proxy_auth_failed"
	case strings.Contains(text, "socks5 connect failed"):
		return "proxy_connect_failed"
	case strings.Contains(text, "proxy"):
		return "proxy_failed"
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return "timeout"
	case strings.Contains(text, "no such host"):
		return "dns_failed"
	case strings.Contains(text, "connection refused"):
		return "connect_refused"
	case strings.Contains(text, "tls"):
		return "tls_failed"
	}
	reason := ChatdFailureReason(text)
	if reason != "" && reason != "chatd_failed" {
		return shared.SafeProxyLogToken(reason, "chatd_failed")
	}
	return "unexpected_failure"
}

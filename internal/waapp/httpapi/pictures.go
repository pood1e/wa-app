package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *dashboardHTTP) handleAccountProfilePicture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	accountID, ok := accountIDFromProfilePicturePath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wa_account_id is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	picture, err := s.service.GetWAAccountProfilePicture(ctx, accountID)
	if err != nil {
		writeProfilePictureError(w, err)
		return
	}
	writeProfilePicture(w, r, picture.ContentType, picture.ProfilePictureID, "private, no-cache", picture.Data)
}

func (s *dashboardHTTP) handleContactProfilePicture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	contactID, ok := contactIDFromProfilePicturePath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contact id is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	picture, err := s.service.GetWAContactProfilePicture(ctx, contactID)
	if err != nil {
		writeProfilePictureError(w, err)
		return
	}
	writeProfilePicture(w, r, picture.ContentType, picture.ProfilePictureID, "private, max-age=3600", picture.Data)
}

func writeProfilePictureError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch shared.ToProtoError(err).GetCode() {
	case waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND:
		w.Header().Set("Cache-Control", "private, max-age=30")
		w.WriteHeader(http.StatusNotFound)
		return
	case waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED:
		status = http.StatusBadRequest
	case waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED:
		status = http.StatusForbidden
	case waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED:
		status = http.StatusServiceUnavailable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	w.Header().Set("Cache-Control", "no-store")
	if status >= 500 {
		w.Header().Set("Retry-After", "5")
	}
	w.WriteHeader(status)
}

func writeProfilePicture(w http.ResponseWriter, r *http.Request, contentType string, etagValue string, cacheControl string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	if etag := safeHTTPETag(etagValue); etag != "" {
		w.Header().Set("ETag", etag)
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func accountIDFromProfilePicturePath(path string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/api/wa/accounts/"), "/profile-picture")
	return parseContactIDPathValue(value)
}

func contactIDFromProfilePicturePath(path string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/api/wa/contacts/"), "/profile-picture")
	return parseContactIDPathValue(value)
}

func contactIDFromContactPath(path string) (string, bool) {
	value := strings.TrimPrefix(path, "/api/wa/contacts/")
	return parseContactIDPathValue(value)
}

func parseContactIDPathValue(value string) (string, bool) {
	value = strings.Trim(value, "/")
	if value == "" || strings.Contains(value, "/") {
		return "", false
	}
	contactID, err := url.PathUnescape(value)
	if err != nil || strings.TrimSpace(contactID) == "" {
		return "", false
	}
	return contactID, true
}

func safeHTTPETag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return `"` + b.String() + `"`
}

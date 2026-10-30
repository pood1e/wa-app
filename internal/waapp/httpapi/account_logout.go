package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleAccountLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	req := &waappv1.LogoutWAAccountRequest{}
	if !readProtoJSONPayload(w, r, 1<<20, req, "request body must be a LogoutWAAccountRequest JSON object") {
		return
	}
	id, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/wa/accounts/"), "/logout"))
	if err != nil || id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wa_account_id is required"})
		return
	}
	req.WaAccountId = id
	ensureAccountSettingsContext(&req.Context, "wa-account-logout")
	resp, err := s.service.LogoutWAAccount(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout WA account failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

package httpapi

import (
	"net/http"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleLongConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.GetLongConnectionStatus(r.Context(), &waappv1.GetLongConnectionStatusRequest{
		Context:              &waappv1.RequestContext{RequestId: newRequestID("wa-conn-status")},
		LoginStateId:         q.Get("login_state_id"),
		WaAccountId:          q.Get("wa_account_id"),
		ClientProfileId:      q.Get("client_profile_id"),
		RegisteredIdentityId: q.Get("registered_identity_id"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load long connection status failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

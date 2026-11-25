package httpapi

import (
	"net/http"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleSetAccountBusinessName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	request := new(waappv1.SetAccountBusinessNameRequest)
	if !readProtoJSONPayload(w, r, 8<<10, request, "request body must contain an account selector and business name") {
		return
	}
	ensureAccountSettingsContext(&request.Context, "wa-account-business")
	response, err := s.service.SetAccountBusinessName(r.Context(), request)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "business name update failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

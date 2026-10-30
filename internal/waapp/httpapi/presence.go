package httpapi

import (
	"context"
	"net/http"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleContactPresence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	request := new(waappv1.SubscribeWAContactPresenceRequest)
	if !readProtoJSONPayload(w, r, 16<<10, request, "invalid presence subscription") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	response, err := s.service.SubscribeWAContactPresence(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load contact presence failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

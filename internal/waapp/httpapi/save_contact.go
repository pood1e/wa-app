package httpapi

import (
	"context"
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleSaveContact(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	req := &waappv1.SaveWAContactRequest{}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil || (protojson.UnmarshalOptions{}).Unmarshal(body, req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be a SaveWAContactRequest JSON object"})
		return
	}
	req.Context = &waappv1.RequestContext{RequestId: newRequestID("wa-contact-save")}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	response, err := s.service.SaveWAContact(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save WA contact failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

package httpapi

import (
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	req := new(waappv1.EditAccountMessageRequest)
	s.handleMessageMutation(w, r, req, func(ctx context.Context) (*waappv1.SendTextMessageResponse, error) {
		return s.service.EditAccountMessage(ctx, req)
	})
}

func (s *dashboardHTTP) handleReactToMessage(w http.ResponseWriter, r *http.Request) {
	req := new(waappv1.ReactToAccountMessageRequest)
	s.handleMessageMutation(w, r, req, func(ctx context.Context) (*waappv1.SendTextMessageResponse, error) {
		return s.service.ReactToAccountMessage(ctx, req)
	})
}

func (s *dashboardHTTP) handleMessageMutation(w http.ResponseWriter, r *http.Request, req proto.Message, operation func(context.Context) (*waappv1.SendTextMessageResponse, error)) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	if !readProtoJSONPayload(w, r, 64<<10, req, "invalid message action") {
		return
	}
	response, err := operation(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "message action failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

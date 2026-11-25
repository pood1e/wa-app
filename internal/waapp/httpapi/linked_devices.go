package httpapi

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleLinkedDevices(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	if r.URL.Path == "/api/wa/linked-devices/unlink" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		linkedDeviceCommand(w, r, new(waappv1.UnlinkDeviceRequest), s.service.UnlinkDevice)
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		response, err := s.service.ListLinkedDevices(ctx, &waappv1.ListLinkedDevicesRequest{WaAccountId: r.URL.Query().Get("wa_account_id")})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load linked devices failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, response)
	case http.MethodPost:
		linkedDeviceCommand(w, r, new(waappv1.LinkDeviceRequest), s.service.LinkDevice)
	default:
		methodNotAllowed(w, "GET, POST")
	}
}

func linkedDeviceCommand[Request proto.Message, Response proto.Message](w http.ResponseWriter, r *http.Request, request Request, execute func(context.Context, Request) (Response, error)) {
	if !readProtoJSONPayload(w, r, 16<<10, request, "invalid linked-device request") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	response, err := execute(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "linked-device operation failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

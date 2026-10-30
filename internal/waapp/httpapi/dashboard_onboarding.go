package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}

func (s *dashboardHTTP) handleAccountAdd(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/wa/account-add"), "/")
	if path == "" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		request := &waappv1.BeginAccountAddRequest{}
		if !readProtoJSONPayload(w, r, 16<<10, request, "invalid account adding request") {
			return
		}
		response, err := s.adding.BeginAccountAdd(ctx, request)
		writeAddingResponse(w, response, err)
		return
	}
	segments := strings.Split(path, "/")
	flowID := segments[0]
	action := strings.Join(segments[1:], "/")
	if action == "" && r.Method == http.MethodGet {
		response, err := s.adding.GetAccountAdd(ctx, &waappv1.AccountAddRequest{FlowId: flowID})
		writeAddingResponse(w, response, err)
		return
	}
	if action == "" && r.Method == http.MethodDelete {
		response, err := s.adding.CancelAccountAdd(ctx, &waappv1.AccountAddRequest{FlowId: flowID})
		writeAddingResponse(w, response, err)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	switch action {
	case "code":
		request := &waappv1.RequestAccountAddCodeRequest{}
		if !readProtoJSONPayload(w, r, 16<<10, request, "invalid verification code request") {
			return
		}
		request.FlowId = flowID
		response, err := s.adding.RequestAccountAddCode(ctx, request)
		writeAddingResponse(w, response, err)
	case "code/submit":
		request := &waappv1.SubmitAccountAddCodeRequest{}
		if !readProtoJSONPayload(w, r, 16<<10, request, "invalid verification submission") {
			return
		}
		request.FlowId = flowID
		response, err := s.adding.SubmitAccountAddCode(ctx, request)
		writeAddingResponse(w, response, err)
	case "password":
		request := &waappv1.SubmitAccountAddPasswordRequest{}
		if !readProtoJSONPayload(w, r, 16<<10, request, "invalid password verification") {
			return
		}
		request.FlowId = flowID
		response, err := s.adding.SubmitAccountAddPassword(ctx, request)
		writeAddingResponse(w, response, err)
	case "deliver":
		s.accountAddCommand(ctx, w, r, flowID, s.adding.DeliverAccountAdd)
	case "transfer":
		s.accountAddCommand(ctx, w, r, flowID, s.adding.RefreshAccountAddTransfer)
	default:
		http.NotFound(w, r)
	}
}

func (s *dashboardHTTP) accountAddCommand(ctx context.Context, w http.ResponseWriter, r *http.Request, flowID string, call func(context.Context, *waappv1.AccountAddRequest, ...grpc.CallOption) (*waappv1.AccountAddResponse, error)) {
	request := &waappv1.AccountAddRequest{}
	if !readProtoJSONPayload(w, r, 16<<10, request, "invalid account adding operation") {
		return
	}
	request.FlowId = flowID
	response, err := call(ctx, request)
	writeAddingResponse(w, response, err)
}

func writeAddingResponse(w http.ResponseWriter, response *waappv1.AccountAddResponse, err error) {
	if err != nil {
		writeProtoJSON(w, http.StatusBadGateway, &waappv1.AccountAddResponse{Error: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, Message: "account adding service is unavailable", Retryable: true}})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

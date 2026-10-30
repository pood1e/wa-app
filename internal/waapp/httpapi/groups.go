package httpapi

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleGroup(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		response, err := s.service.GetGroup(ctx, groupGetRequest(r))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load group failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, response)
	case http.MethodPost:
		groupCommand(w, r, new(waappv1.CreateGroupRequest), s.service.CreateGroup)
	case http.MethodPatch:
		groupCommand(w, r, new(waappv1.UpdateGroupRequest), s.service.UpdateGroup)
	default:
		methodNotAllowed(w, "GET, POST, PATCH")
	}
}

func groupCommand[Request proto.Message, Response proto.Message](w http.ResponseWriter, r *http.Request, request Request, execute func(context.Context, Request) (Response, error)) {
	if !readProtoJSONPayload(w, r, 1<<20, request, "invalid group request") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	response, err := execute(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "group operation failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, response)
}

func (s *dashboardHTTP) handleGroupAction(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	if r.URL.Path == "/api/wa/groups/requests" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		response, err := s.service.ListGroupJoinRequests(ctx, groupGetRequest(r))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load group join requests failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, response)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	switch r.URL.Path {
	case "/api/wa/groups/participants":
		groupCommand(w, r, new(waappv1.UpdateGroupParticipantsRequest), s.service.UpdateGroupParticipants)
	case "/api/wa/groups/invite":
		groupCommand(w, r, new(waappv1.GetGroupInviteRequest), s.service.GetGroupInvite)
	case "/api/wa/groups/inspect-invite":
		groupCommand(w, r, new(waappv1.GroupInviteRequest), s.service.InspectGroupInvite)
	case "/api/wa/groups/join":
		groupCommand(w, r, new(waappv1.GroupInviteRequest), s.service.JoinGroup)
	case "/api/wa/groups/leave":
		groupCommand(w, r, new(waappv1.GetGroupRequest), s.service.LeaveGroup)
	case "/api/wa/groups/sync":
		groupCommand(w, r, new(waappv1.ListJoinedGroupsRequest), s.service.ListJoinedGroups)
	default:
		http.NotFound(w, r)
	}
}

func groupGetRequest(r *http.Request) *waappv1.GetGroupRequest {
	return &waappv1.GetGroupRequest{Context: &waappv1.RequestContext{RequestId: newRequestID("wa-group")}, WaAccountId: r.URL.Query().Get("wa_account_id"), GroupRef: r.URL.Query().Get("group_ref")}
}

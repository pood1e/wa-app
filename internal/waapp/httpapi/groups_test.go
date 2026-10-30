package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func TestGroupHTTPRejectsAmbiguousChangesBeforeExecution(t *testing.T) {
	for _, body := range []string{`{"name":"new","locked":false}`, `{"unsupported":true}`, strings.Repeat(" ", (1<<20)+1)} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/wa/groups", strings.NewReader(body))
		called := false
		groupCommand(response, request, new(waappv1.UpdateGroupRequest), func(context.Context, *waappv1.UpdateGroupRequest) (*waappv1.GroupMutationResponse, error) {
			called = true
			return &waappv1.GroupMutationResponse{}, nil
		})
		if response.Code != http.StatusBadRequest || called {
			t.Fatal("malformed group change reached protocol execution")
		}
	}
}

func TestGroupHTTPPreservesExplicitFalseAndCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodPatch, "/api/wa/groups", strings.NewReader(`{"locked":false}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	called := false
	groupCommand(response, request, new(waappv1.UpdateGroupRequest), func(execution context.Context, change *waappv1.UpdateGroupRequest) (*waappv1.GroupMutationResponse, error) {
		called = true
		locked, ok := change.GetChange().(*waappv1.UpdateGroupRequest_Locked)
		if !ok || locked.Locked {
			t.Fatal("explicit false was discarded from the group change oneof")
		}
		deadline, _ := execution.Deadline()
		callerDeadline, _ := ctx.Deadline()
		if deadline.After(callerDeadline) {
			t.Fatal("group command extended the caller's deadline")
		}
		return &waappv1.GroupMutationResponse{}, nil
	})
	if response.Code != http.StatusOK || !called {
		t.Fatal("valid group change was not executed")
	}
}

func TestGroupJoinRequestHTTPAdvertisesReadMethod(t *testing.T) {
	handler := &dashboardHTTP{service: account.New(nil, nil, nil, nil, nil, nil)}
	response := httptest.NewRecorder()
	handler.handleGroupAction(response, httptest.NewRequest(http.MethodPost, "/api/wa/groups/requests", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatal("join-request resource did not advertise GET")
	}
}

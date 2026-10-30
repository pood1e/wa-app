package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type addingFixture struct {
	waappv1.WaAccountAddServiceClient
	begin *waappv1.BeginAccountAddRequest
	code  *waappv1.RequestAccountAddCodeRequest
}

func (f *addingFixture) BeginAccountAdd(_ context.Context, req *waappv1.BeginAccountAddRequest, _ ...grpc.CallOption) (*waappv1.AccountAddResponse, error) {
	f.begin = req
	return &waappv1.AccountAddResponse{}, nil
}
func (f *addingFixture) RequestAccountAddCode(_ context.Context, req *waappv1.RequestAccountAddCodeRequest, _ ...grpc.CallOption) (*waappv1.AccountAddResponse, error) {
	f.code = req
	return &waappv1.AccountAddResponse{}, nil
}

func TestAccountAddingHTTPContract(t *testing.T) {
	fixture := &addingFixture{}
	handler := &dashboardHTTP{adding: fixture}
	request := httptest.NewRequest(http.MethodPost, "/api/wa/account-add", strings.NewReader(`{"context":{"request_id":"begin"},"phone":{"e164_number":"+12025550123"},"source":{"transfer":{}}}`))
	result := httptest.NewRecorder()
	handler.handleAccountAdd(result, request)
	if result.Code != http.StatusOK || fixture.begin.GetSource().GetTransfer() == nil {
		t.Fatal("HTTP adapter lost the source oneof")
	}
	request = httptest.NewRequest(http.MethodPost, "/api/wa/account-add", strings.NewReader(`{"phone":{},"source":{"registration":{},"transfer":{}}}`))
	result = httptest.NewRecorder()
	handler.handleAccountAdd(result, request)
	if result.Code != http.StatusBadRequest {
		t.Fatal("HTTP adapter accepted contradictory source variants")
	}
	request = httptest.NewRequest(http.MethodPost, "/api/wa/account-add/route-flow/code", strings.NewReader(`{"flow_id":"body-flow","context":{"request_id":"code"},"delivery_method":"VERIFICATION_DELIVERY_METHOD_SMS"}`))
	result = httptest.NewRecorder()
	handler.handleAccountAdd(result, request)
	if result.Code != http.StatusOK || fixture.code.GetFlowId() != "route-flow" || fixture.code.GetContext().GetRequestId() != "code" {
		t.Fatal("HTTP adapter changed operation identity or ignored the route")
	}
	request = httptest.NewRequest(http.MethodPost, "/api/wa/account-add", strings.NewReader(`{"unexpected_field":true}`))
	result = httptest.NewRecorder()
	handler.handleAccountAdd(result, request)
	if result.Code != http.StatusBadRequest {
		t.Fatal("HTTP adapter accepted an obsolete payload")
	}
}

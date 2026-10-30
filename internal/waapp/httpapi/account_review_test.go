package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type reviewRepository struct{ account.Repository }

func (reviewRepository) GetWAAccount(_ context.Context, id string) (*waappv1.WAAccount, error) {
	return &waappv1.WAAccount{WaAccountId: id}, nil
}

func (reviewRepository) GetLatestAccountLoginState(_ context.Context, id string) (*waappv1.LoginState, error) {
	return &waappv1.LoginState{WaAccountId: id, ClientProfileId: "profile-" + id, Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID, LastError: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_AUTHENTICATION_FAILED}}, nil
}

type reviewProtocol struct {
	wacore.Communication
	profileID string
	refresh   bool
	started   chan struct{}
	release   chan struct{}
}

func (p *reviewProtocol) GetAccountReview(ctx context.Context, id string, refresh bool) (*protocolv1.AccountReview, error) {
	p.profileID, p.refresh = id, refresh
	if p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &protocolv1.AccountReview{Status: protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW}, nil
}

func (p *reviewProtocol) SubmitAccountReview(_ context.Context, id, _ string) (*protocolv1.AccountReview, error) {
	p.profileID = id
	return &protocolv1.AccountReview{Status: protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW}, nil
}

func TestAccountReviewHTTPContract(t *testing.T) {
	protocol := new(reviewProtocol)
	ephemeral := runtime.NewMemoryRuntime()
	defer ephemeral.Close()
	handler := &dashboardHTTP{service: account.New(reviewRepository{}, ephemeral, protocol, nil, nil, nil)}
	for _, tc := range []struct {
		method, suffix, body string
		status               int
		refresh              bool
	}{
		{http.MethodGet, "", "", http.StatusOK, false},
		{http.MethodPost, "/refresh", "", http.StatusOK, true},
		{http.MethodPost, "", `{"wa_account_id":"waacc_other","description":{"value":"synthetic request"}}`, http.StatusOK, true},
		{http.MethodPost, "", `{"unexpected_field":true}`, http.StatusBadRequest, true},
		{http.MethodGet, "/refresh", "", http.StatusMethodNotAllowed, true},
		{http.MethodDelete, "", "", http.StatusMethodNotAllowed, true},
	} {
		r := httptest.NewRequest(tc.method, "/api/wa/accounts/waacc_route/review"+tc.suffix, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		handler.handleAccount(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.suffix, w.Code, tc.status)
		}
		if tc.status == http.StatusOK {
			response := new(waappv1.AccountReviewResponse)
			if err := protojson.Unmarshal(w.Body.Bytes(), response); err != nil || response.GetError() != nil || response.GetReview().GetStatus() != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW {
				t.Fatal("review response did not follow the protobuf contract")
			}
			if protocol.profileID != "profile-waacc_route" || protocol.refresh != tc.refresh || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("review ignored route authority, refresh semantics or private cache policy")
			}
		}
	}
}

func TestConcurrentReviewOperationsAreRejected(t *testing.T) {
	protocol := &reviewProtocol{started: make(chan struct{}), release: make(chan struct{})}
	ephemeral := runtime.NewMemoryRuntime()
	defer ephemeral.Close()
	app := account.New(reviewRepository{}, ephemeral, protocol, nil, nil, nil)
	done := make(chan *waappv1.AccountReviewResponse, 1)
	go func() {
		result, _ := app.GetAccountReview(t.Context(), &waappv1.GetAccountReviewRequest{WaAccountId: "waacc_route", RemoteRefresh: true})
		done <- result
	}()
	<-protocol.started
	result, err := app.SubmitAccountReview(t.Context(), &waappv1.SubmitAccountReviewRequest{WaAccountId: "waacc_route", Description: &waappv1.SensitiveText{Value: "synthetic request"}})
	close(protocol.release)
	first := <-done
	if err != nil || result.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT || first.GetError() != nil {
		t.Fatal("concurrent review submission bypassed account ownership")
	}
}

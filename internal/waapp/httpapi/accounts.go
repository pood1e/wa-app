package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListWAAccounts(r.Context(), &waappv1.ListWAAccountsRequest{
		Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-list")},
		Limit:   int32(positiveInt(q.Get("limit"), 100)),
		Cursor:  q.Get("cursor"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA accounts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleAccount(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/profile-picture") {
		s.handleAccountProfilePicture(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/logout") {
		s.handleAccountLogout(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/proxy") {
		s.handleAccountProxy(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "/review") {
		s.handleAccountReview(w, r)
		return
	}
	accountID, err := url.PathUnescape(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/wa/accounts/"), "/"))
	if err != nil || accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wa_account_id is required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		resp, err := s.service.GetWAAccount(r.Context(), &waappv1.GetWAAccountRequest{Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-get")}, WaAccountId: accountID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA account failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, resp)
	case http.MethodDelete:
		resp, err := s.service.DeleteWAAccount(r.Context(), &waappv1.DeleteWAAccountRequest{Context: &waappv1.RequestContext{RequestId: newRequestID("wa-account-delete")}, WaAccountId: accountID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA account failed"})
			return
		}
		writeProtoJSON(w, http.StatusOK, resp)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodDelete)
	}
}

func (s *dashboardHTTP) handleAccountOTPMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListAccountOtpMessages(r.Context(), &waappv1.ListAccountOtpMessagesRequest{
		Context: &waappv1.RequestContext{
			RequestId: newRequestID("wa-otp-list"),
		},
		WaAccountId:            q.Get("wa_account_id"),
		Limit:                  int32(positiveInt(q.Get("limit"), 20)),
		Cursor:                 q.Get("cursor"),
		IncludeSensitiveValues: queryBool(q.Get("include_sensitive_values"), true),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA OTP history failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

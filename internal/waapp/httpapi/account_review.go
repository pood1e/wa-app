package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleAccountReview(w http.ResponseWriter, r *http.Request) {
	path, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/wa/accounts/"))
	id, action, valid := strings.Cut(path, "/review")
	if err != nil || !valid || id == "" || strings.Contains(id, "/") || (action != "" && action != "/refresh") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account review path"})
		return
	}
	var response *waappv1.AccountReviewResponse
	if r.Method == http.MethodGet && action == "" {
		response, err = s.service.GetAccountReview(r.Context(), &waappv1.GetAccountReviewRequest{WaAccountId: id})
	} else if r.Method == http.MethodPost && action == "/refresh" {
		response, err = s.service.GetAccountReview(r.Context(), &waappv1.GetAccountReviewRequest{WaAccountId: id, RemoteRefresh: true})
	} else if r.Method == http.MethodPost && action == "" {
		request := new(waappv1.SubmitAccountReviewRequest)
		if !readProtoJSONPayload(w, r, 16<<10, request, "request body must contain a review description") {
			return
		}
		request.WaAccountId = id
		response, err = s.service.SubmitAccountReview(r.Context(), request)
	} else {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account review operation failed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeProtoJSON(w, http.StatusOK, response)
}

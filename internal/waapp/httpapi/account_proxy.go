package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleAccountProxy(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/wa/accounts/"), "/proxy"))
	if err != nil || id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account proxy path"})
		return
	}
	var response *waappv1.AccountProxyResponse
	switch r.Method {
	case http.MethodGet:
		response, err = s.service.GetAccountProxy(r.Context(), &waappv1.GetAccountProxyRequest{WaAccountId: id})
	case http.MethodPut:
		request := new(waappv1.SetAccountProxyRequest)
		if !readProtoJSONPayload(w, r, 8<<10, request, "request body must contain an account proxy configuration") {
			return
		}
		request.WaAccountId = id
		response, err = s.service.SetAccountProxy(r.Context(), request)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account proxy operation failed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeProtoJSON(w, http.StatusOK, response)
}

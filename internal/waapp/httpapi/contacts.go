package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleSaveContact(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListWAContacts(r.Context(), &waappv1.ListWAContactsRequest{
		Context:     &waappv1.RequestContext{RequestId: newRequestID("wa-contact-list")},
		WaAccountId: q.Get("wa_account_id"),
		Limit:       int32(positiveInt(q.Get("limit"), 500)),
		Cursor:      q.Get("cursor"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA contacts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleContactResource(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/profile-picture") {
		s.handleContactProfilePicture(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		s.handleDeleteContact(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *dashboardHTTP) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	contactID, ok := contactIDFromContactPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contact id is required"})
		return
	}
	accountID := r.URL.Query().Get("wa_account_id")
	if accountID == "" && r.ContentLength != 0 {
		payload, ok := readJSONPayload(w, r)
		if !ok {
			return
		}
		accountID = textField(payload, "wa_account_id")
	}
	resp, err := s.service.DeleteWAContact(r.Context(), &waappv1.DeleteWAContactRequest{
		Context:     &waappv1.RequestContext{RequestId: newRequestID("wa-contact-delete")},
		WaAccountId: accountID,
		ContactRef:  contactID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA contact failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleResolveContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	req := &waappv1.ResolveWAContactsRequest{}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be a ResolveWAContactsRequest JSON object"})
			return
		}
	}
	q := r.URL.Query()
	if req.WaAccountId == "" {
		req.WaAccountId = q.Get("wa_account_id")
	}
	if req.Limit == 0 {
		req.Limit = int32(positiveInt(q.Get("limit"), 50))
	}
	if req.Context == nil {
		req.Context = &waappv1.RequestContext{RequestId: newRequestID("wa-contact-resolve")}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	resp, err := s.service.ResolveWAContacts(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "resolve WA contacts failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

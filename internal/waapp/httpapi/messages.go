package httpapi

import (
	"net/http"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	q := r.URL.Query()
	resp, err := s.service.ListAccountMessages(r.Context(), &waappv1.ListAccountMessagesRequest{
		Context:              &waappv1.RequestContext{RequestId: newRequestID("wa-message-list")},
		WaAccountId:          q.Get("wa_account_id"),
		ContactRef:           q.Get("contact_ref"),
		Limit:                int32(positiveInt(q.Get("limit"), 100)),
		Cursor:               q.Get("cursor"),
		IncludeSensitiveText: true,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load WA messages failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleMarkMessagesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.MarkAccountMessagesRead(r.Context(), &waappv1.MarkAccountMessagesReadRequest{
		Context:           &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-read"))},
		WaAccountId:       textField(payload, "wa_account_id"),
		AccountMessageIds: stringListField(payload, "account_message_ids"),
		LocalOnly:         boolField(payload, "local_only"),
		ContactRef:        textField(payload, "contact_ref"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mark WA messages read failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleDeleteMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.DeleteAccountMessages(r.Context(), &waappv1.DeleteAccountMessagesRequest{
		Context:           &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-delete"))},
		WaAccountId:       textField(payload, "wa_account_id"),
		AccountMessageIds: stringListField(payload, "account_message_ids"),
		Mode:              deleteModeField(payload),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete WA messages failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSendTextMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload, ok := readJSONPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.SendTextMessage(r.Context(), &waappv1.SendTextMessageRequest{
		Context:                 &waappv1.RequestContext{RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID("wa-message-send"))},
		WaAccountId:             textField(payload, "wa_account_id"),
		ContactRef:              textField(payload, "contact_ref"),
		Text:                    &waappv1.SensitiveText{Value: textField(payload, "text")},
		ClientMessageId:         textField(payload, "client_message_id"),
		ReplyToAccountMessageId: textField(payload, "reply_to_account_message_id"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "send WA text message failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

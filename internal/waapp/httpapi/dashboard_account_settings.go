package httpapi

import (
	"net/http"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleGetTwoFactorAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return
	}
	payload := queryPayload(r)
	resp, err := s.service.GetTwoFactorAuthStatus(r.Context(), &waappv1.GetTwoFactorAuthStatusRequest{
		Context:       &waappv1.RequestContext{RequestId: newRequestID("wa-account-2fa-status")},
		Selector:      accountSettingsSelector(payload),
		RemoteRefresh: boolField(payload, "remote_refresh"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get WA 2FA status failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSetTwoFactorAuthSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	password, _ := payload["password"].(string)
	resp, err := s.service.SetTwoFactorAuthSettings(r.Context(), &waappv1.SetTwoFactorAuthSettingsRequest{
		Context:  accountSettingsRequestContext(payload, "wa-account-2fa"),
		Selector: accountSettingsSelector(payload),
		Password: &waappv1.SensitiveText{Value: password},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA 2FA settings failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSetAccountEmail(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.SetAccountEmail(r.Context(), &waappv1.SetAccountEmailRequest{
		Context:       accountSettingsRequestContext(payload, "wa-account-email"),
		Selector:      accountSettingsSelector(payload),
		EmailAddress:  textField(payload, "email_address"),
		GoogleIdToken: &waappv1.SensitiveText{Value: textField(payload, "google_id_token")},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA account email failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleRequestAccountEmailOtp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.RequestAccountEmailOtp(r.Context(), &waappv1.RequestAccountEmailOtpRequest{
		Context:        accountSettingsRequestContext(payload, "wa-account-email-otp"),
		Selector:       accountSettingsSelector(payload),
		LocaleLanguage: firstNonEmpty(textField(payload, "locale_language"), textField(payload, "language"), "en"),
		LocaleCountry:  firstNonEmpty(textField(payload, "locale_country"), textField(payload, "country"), "US"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request WA account email OTP failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleVerifyAccountEmailOtp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.VerifyAccountEmailOtp(r.Context(), &waappv1.VerifyAccountEmailOtpRequest{
		Context:  accountSettingsRequestContext(payload, "wa-account-email-otp-verify"),
		Selector: accountSettingsSelector(payload),
		Code:     &waappv1.SensitiveText{Value: firstNonEmpty(textField(payload, "code"), textField(payload, "otp"))},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "verify WA account email OTP failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSetAccountProfileName(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	req := &waappv1.SetAccountProfileNameRequest{}
	if !readProtoJSONPayload(w, r, 1<<20, req, "request body must be a SetAccountProfileNameRequest JSON object") {
		return
	}
	ensureAccountSettingsContext(&req.Context, "wa-account-profile-name")
	resp, err := s.service.SetAccountProfileName(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA account profile name failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSetAccountProfilePicture(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	req := &waappv1.SetAccountProfilePictureRequest{}
	if !readProtoJSONPayload(w, r, 4<<20, req, "request body must be a SetAccountProfilePictureRequest JSON object") {
		return
	}
	ensureAccountSettingsContext(&req.Context, "wa-account-profile-picture")
	resp, err := s.service.SetAccountProfilePicture(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA account profile picture failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleRemoveAccountProfilePicture(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	req := &waappv1.RemoveAccountProfilePictureRequest{}
	if !readProtoJSONPayload(w, r, 1<<20, req, "request body must be a RemoveAccountProfilePictureRequest JSON object") {
		return
	}
	ensureAccountSettingsContext(&req.Context, "wa-account-profile-picture-remove")
	resp, err := s.service.RemoveAccountProfilePicture(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "remove WA account profile picture failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) requireAccountSettingsPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return false
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return false
	}
	return true
}

func readAccountSettingsPayload(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	return readJSONPayload(w, r)
}

func queryPayload(r *http.Request) map[string]any {
	payload := map[string]any{}
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			payload[key] = values[len(values)-1]
		}
	}
	return payload
}

func accountSettingsRequestContext(payload map[string]any, prefix string) *waappv1.RequestContext {
	return &waappv1.RequestContext{
		RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID(prefix)),
	}
}

func ensureAccountSettingsContext(ctx **waappv1.RequestContext, prefix string) {
	if *ctx == nil {
		*ctx = &waappv1.RequestContext{}
	}
	if (*ctx).RequestId == "" {
		(*ctx).RequestId = newRequestID(prefix)
	}
}

func accountSettingsSelector(payload map[string]any) *waappv1.AccountLoginSelector {
	selector := objectField(payload, "selector")
	return &waappv1.AccountLoginSelector{
		LoginStateId:         firstNonEmpty(textField(payload, "login_state_id"), textField(selector, "login_state_id")),
		RegisteredIdentityId: firstNonEmpty(textField(payload, "registered_identity_id"), textField(selector, "registered_identity_id")),
		WaAccountId:          firstNonEmpty(textField(payload, "wa_account_id"), textField(selector, "wa_account_id")),
		ClientProfileId:      firstNonEmpty(textField(payload, "client_profile_id"), textField(selector, "client_profile_id")),
	}
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const contactProfilePictureCacheTTL = 6 * time.Hour
const profilePictureFailureCacheTTL = 10 * time.Minute
const contactProfilePictureLatestCacheVersion = "latest"

type waContactProfilePictureResolver interface {
	ResolveContactProfilePicture(context.Context, EngineContactProfilePictureInput) EngineContactProfilePictureResult
}

type WAContactProfilePicture struct {
	ProfilePictureID string
	ContentType      string
	Data             []byte
}

type contactProfilePictureCacheEntry struct {
	ProfilePictureID string `json:"profile_picture_id"`
	ContentType      string `json:"content_type"`
	Data             []byte `json:"data"`
}

func (s *Server) contactProfilePictureRunner(ctx context.Context, loginState *waappv1.LoginState) (ProtocolEngine, func(), error) {
	if s != nil && s.longConnections != nil {
		if runner := s.longConnections.Runner(loginState); runner != nil {
			if _, ok := runner.(waContactProfilePictureResolver); ok {
				return runner, func() {}, nil
			}
		}
	}
	return s.contactResolverRunner(loginState)
}

func contactProfilePictureRemoteTimeout(runner ProtocolEngine) time.Duration {
	if _, ok := runner.(*longConnectionNativeEngine); ok {
		return longConnectionWaitTimeout + defaultContactProfilePictureTimeout
	}
	return defaultContactProfilePictureTimeout
}

func (s *Server) GetAccountProfilePicture(ctx context.Context, req *waappv1.GetAccountProfilePictureRequest) (*waappv1.GetAccountProfilePictureResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetAccountProfilePictureResponse{Error: ToProtoError(err)}, nil
	}
	picture, err := s.getWAAccountProfilePicture(ctx, req.GetSelector())
	if err != nil {
		return &waappv1.GetAccountProfilePictureResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetAccountProfilePictureResponse{Image: picture.Data, ContentType: picture.ContentType, ProfilePictureId: picture.ProfilePictureID}, nil
}

func (s *Server) GetWAAccountProfilePicture(ctx context.Context, accountID string) (WAContactProfilePicture, error) {
	return s.getWAAccountProfilePicture(ctx, &waappv1.AccountLoginSelector{WaAccountId: accountID})
}

func (s *Server) getWAAccountProfilePicture(ctx context.Context, selector *waappv1.AccountLoginSelector) (WAContactProfilePicture, error) {
	loginState, err := s.accountSettingsLoginState(ctx, selector)
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	account, err := s.store.GetWAAccount(ctx, loginState.GetWaAccountId())
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	if cached, ok := s.cachedWAAccountProfilePicture(ctx, account.GetWaAccountId()); ok {
		return cached, nil
	}
	accountCacheKey := accountProfilePictureCacheKey(account.GetWaAccountId())
	if s.cachedWAProfilePictureFailure(ctx, accountCacheKey) {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	pnJID := accountProfilePictureJID(account)
	if pnJID == "" {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA account phone is required", false)
	}
	runner, release, err := s.contactProfilePictureRunner(ctx, loginState)
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	defer release()
	resolver, ok := runner.(waContactProfilePictureResolver)
	if !ok {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA account profile picture resolver is not configured", false)
	}
	remoteTimeout := contactProfilePictureRemoteTimeout(runner)
	result := resolver.ResolveContactProfilePicture(ctx, EngineContactProfilePictureInput{
		WAAccountID:          loginState.GetWaAccountId(),
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		AppVersion:           s.loginStateAppVersion(ctx, loginState),
		ContactJID:           pnJID,
		ContactPNJID:         pnJID,
		RemoteTimeout:        remoteTimeout,
	})
	if result.Err != nil {
		if shouldCacheProfilePictureFailure(result.Err) {
			s.cacheWAProfilePictureFailure(ctx, accountCacheKey)
		}
		logWAProfilePictureError("account", result.Err)
		return WAContactProfilePicture{}, result.Err
	}
	picture := WAContactProfilePicture{ProfilePictureID: result.ProfilePictureID, ContentType: result.ContentType, Data: result.Data}
	s.cacheWAAccountProfilePicture(ctx, account.GetWaAccountId(), picture)
	return picture, nil
}

func (s *Server) GetWAContactProfilePicture(ctx context.Context, contactID string) (WAContactProfilePicture, error) {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA contact id is required", false)
	}
	contact, err := s.store.GetWAContact(ctx, contactID)
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	contactCacheKey := contactProfilePictureCacheKey(contact.GetContactId(), contactProfilePictureCacheVersion(contact.GetProfilePictureId()))
	if cached, ok := s.cachedWAContactProfilePicture(ctx, contact); ok {
		return cached, nil
	}
	if s.cachedWAProfilePictureFailure(ctx, contactCacheKey) {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	loginState, err := s.activeContactResolveLoginState(ctx, contact.GetWaAccountId())
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	runner, release, err := s.contactProfilePictureRunner(ctx, loginState)
	if err != nil {
		return WAContactProfilePicture{}, err
	}
	defer release()
	resolver, ok := runner.(waContactProfilePictureResolver)
	if !ok {
		return WAContactProfilePicture{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA contact profile picture resolver is not configured", false)
	}
	remoteTimeout := contactProfilePictureRemoteTimeout(runner)
	result := resolver.ResolveContactProfilePicture(ctx, EngineContactProfilePictureInput{
		WAAccountID:          contact.GetWaAccountId(),
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		AppVersion:           s.loginStateAppVersion(ctx, loginState),
		ContactJID:           contact.GetJid(),
		ContactPNJID:         phoneNumberWAJID(contact.GetNumber()),
		ContactPictureID:     contact.GetProfilePictureId(),
		RemoteTimeout:        remoteTimeout,
	})
	if result.Err != nil {
		if shouldCacheProfilePictureFailure(result.Err) {
			s.cacheWAProfilePictureFailure(ctx, contactCacheKey)
		}
		logWAProfilePictureError("contact", result.Err)
		return WAContactProfilePicture{}, result.Err
	}
	if result.ProfilePictureID != "" && result.ProfilePictureID != contact.GetProfilePictureId() {
		contact.ProfilePictureId = result.ProfilePictureID
		_ = s.store.SaveWAContacts(ctx, []*waappv1.WAContact{contact})
	}
	picture := WAContactProfilePicture{
		ProfilePictureID: firstNonEmpty(result.ProfilePictureID, contact.GetProfilePictureId()),
		ContentType:      result.ContentType,
		Data:             result.Data,
	}
	s.cacheWAContactProfilePicture(ctx, contact.GetContactId(), picture)
	return picture, nil
}

func (s *Server) cachedWAAccountProfilePicture(ctx context.Context, accountID string) (WAContactProfilePicture, bool) {
	return s.cachedWAProfilePicture(ctx, accountProfilePictureCacheKey(accountID))
}

func (s *Server) cachedWAContactProfilePicture(ctx context.Context, contact *waappv1.WAContact) (WAContactProfilePicture, bool) {
	if s == nil || s.runtime == nil || contact == nil || contact.GetContactId() == "" {
		return WAContactProfilePicture{}, false
	}
	version := contactProfilePictureCacheVersion(contact.GetProfilePictureId())
	picture, ok := s.cachedWAProfilePicture(ctx, contactProfilePictureCacheKey(contact.GetContactId(), version))
	if picture.ProfilePictureID == "" {
		picture.ProfilePictureID = contact.GetProfilePictureId()
	}
	return picture, ok
}

func (s *Server) cachedWAProfilePicture(ctx context.Context, key string) (WAContactProfilePicture, bool) {
	if s == nil || s.runtime == nil || strings.TrimSpace(key) == "" {
		return WAContactProfilePicture{}, false
	}
	data, err := s.runtime.GetTransientState(ctx, key)
	if err != nil || len(data) == 0 {
		return WAContactProfilePicture{}, false
	}
	var entry contactProfilePictureCacheEntry
	if json.Unmarshal(data, &entry) != nil || len(entry.Data) == 0 || entry.ContentType == "" {
		return WAContactProfilePicture{}, false
	}
	return WAContactProfilePicture{ProfilePictureID: entry.ProfilePictureID, ContentType: entry.ContentType, Data: entry.Data}, true
}

func (s *Server) cacheWAAccountProfilePicture(ctx context.Context, accountID string, picture WAContactProfilePicture) {
	s.cacheWAProfilePicture(ctx, accountProfilePictureCacheKey(accountID), picture)
}

func (s *Server) cacheWAContactProfilePicture(ctx context.Context, contactID string, picture WAContactProfilePicture) {
	s.cacheWAProfilePicture(ctx, contactProfilePictureCacheKey(contactID, contactProfilePictureCacheVersion(picture.ProfilePictureID)), picture)
}

func (s *Server) cacheWAProfilePicture(ctx context.Context, key string, picture WAContactProfilePicture) {
	if s == nil || s.runtime == nil || strings.TrimSpace(key) == "" || len(picture.Data) == 0 {
		return
	}
	data, err := json.Marshal(contactProfilePictureCacheEntry{ProfilePictureID: picture.ProfilePictureID, ContentType: picture.ContentType, Data: picture.Data})
	if err != nil {
		return
	}
	_ = s.runtime.SaveTransientState(ctx, key, data, contactProfilePictureCacheTTL)
	_ = s.runtime.DeleteTransientState(ctx, profilePictureFailureCacheKey(key))
}

func (s *Server) cachedWAProfilePictureFailure(ctx context.Context, key string) bool {
	if s == nil || s.runtime == nil || strings.TrimSpace(key) == "" {
		return false
	}
	data, err := s.runtime.GetTransientState(ctx, profilePictureFailureCacheKey(key))
	return err == nil && len(data) > 0
}

func (s *Server) cacheWAProfilePictureFailure(ctx context.Context, key string) {
	if s == nil || s.runtime == nil || strings.TrimSpace(key) == "" {
		return
	}
	_ = s.runtime.SaveTransientState(ctx, profilePictureFailureCacheKey(key), []byte("1"), profilePictureFailureCacheTTL)
}

func profilePictureFailureCacheKey(key string) string {
	return key + ":failure"
}

func shouldCacheProfilePictureFailure(err error) bool {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		return false
	}
	if appErr == nil {
		return false
	}
	return appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND
}

func (s *Server) deleteWAAccountProfilePictureCache(ctx context.Context, accountID string) {
	if s == nil || s.runtime == nil || accountID == "" {
		return
	}
	key := accountProfilePictureCacheKey(accountID)
	_ = s.runtime.DeleteTransientState(ctx, key)
	_ = s.runtime.DeleteTransientState(ctx, profilePictureFailureCacheKey(key))
}

func accountProfilePictureCacheKey(accountID string) string {
	return "wa-account-profile-picture:" + strings.TrimSpace(accountID)
}

func contactProfilePictureCacheKey(contactID string, profilePictureID string) string {
	return "wa-contact-profile-picture:" + contactID + ":" + profilePictureID
}

func contactProfilePictureCacheVersion(profilePictureID string) string {
	profilePictureID = strings.TrimSpace(profilePictureID)
	if profilePictureID == "" {
		return contactProfilePictureLatestCacheVersion
	}
	return profilePictureID
}

func accountProfilePictureJID(account *waappv1.WAAccount) string {
	if account == nil {
		return ""
	}
	phone := normalizePhone(account.GetPhone())
	digits := digitsOnly(phone.GetE164Number())
	if digits == "" {
		digits = digitsOnly(phone.GetCountryCallingCode() + phone.GetNationalNumber())
	}
	return normalizeWAJID(digits)
}

func IsWAProfilePictureNotFound(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND
}

func IsWAContactProfilePictureNotFound(err error) bool {
	return IsWAProfilePictureNotFound(err)
}

func logWAProfilePictureError(scope string, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		log.Printf("WA %s profile picture fetch failed code=%s retryable=%t", safeProxyLogToken(scope, "profile"), appErr.Code.String(), appErr.Retryable)
		return
	}
	log.Printf("WA %s profile picture fetch failed code=%s retryable=false reason=%s", safeProxyLogToken(scope, "profile"), waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL.String(), contactProfilePictureFailureReason(err))
}

func contactProfilePictureFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "username/password authentication failed"):
		return "proxy_auth_failed"
	case strings.Contains(text, "socks5 connect failed"):
		return "proxy_connect_failed"
	case strings.Contains(text, "proxy"):
		return "proxy_failed"
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return "timeout"
	case strings.Contains(text, "no such host"):
		return "dns_failed"
	case strings.Contains(text, "connection refused"):
		return "connect_refused"
	case strings.Contains(text, "tls"):
		return "tls_failed"
	}
	reason := chatdFailureReason(text)
	if reason != "" && reason != "chatd_failed" {
		return safeProxyLogToken(reason, "chatd_failed")
	}
	return "unexpected_failure"
}

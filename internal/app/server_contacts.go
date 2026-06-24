package app

import (
	"context"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const defaultContactResolveLimit = 50

type waContactResolver interface {
	ResolveContacts(context.Context, EngineContactResolveInput) EngineContactResolveResult
}

func (s *Server) ListWAContacts(ctx context.Context, req *waappv1.ListWAContactsRequest) (*waappv1.ListWAContactsResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ListWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.ListWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	contacts, nextCursor, err := s.store.ListWAContacts(ctx, accountID, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.ListWAContactsResponse{Contacts: contacts, NextCursor: nextCursor}, nil
}

func (s *Server) ResolveWAContacts(ctx context.Context, req *waappv1.ResolveWAContactsRequest) (*waappv1.ResolveWAContactsResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	jids, err := s.resolveContactJIDs(ctx, accountID, req.GetJids(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	if len(jids) == 0 {
		return &waappv1.ResolveWAContactsResponse{}, nil
	}
	loginState, err := s.activeContactResolveLoginState(ctx, accountID)
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	runner, release, err := s.contactResolverRunner(loginState)
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(err)}, nil
	}
	defer release()
	resolver, ok := runner.(waContactResolver)
	if !ok {
		return &waappv1.ResolveWAContactsResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA contact resolver is not configured", false))}, nil
	}
	result := resolver.ResolveContacts(ctx, EngineContactResolveInput{
		WAAccountID:          accountID,
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		AppVersion:           s.loginStateAppVersion(ctx, loginState),
		JIDs:                 jids,
		RemoteTimeout:        defaultContactUsyncTimeout,
	})
	if len(result.Contacts) > 0 {
		_ = s.store.SaveWAContacts(ctx, result.Contacts)
	}
	if result.Err != nil {
		return &waappv1.ResolveWAContactsResponse{Contacts: result.Contacts, QueriedCount: int32(result.Queried), ResolvedCount: int32(result.Resolved), Error: ToProtoError(result.Err)}, nil
	}
	return &waappv1.ResolveWAContactsResponse{Contacts: result.Contacts, QueriedCount: int32(result.Queried), ResolvedCount: int32(result.Resolved)}, nil
}

func (s *Server) DeleteWAContact(ctx context.Context, req *waappv1.DeleteWAContactRequest) (*waappv1.DeleteWAContactResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.DeleteWAContactResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteWAContactResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.DeleteWAContactResponse{Error: ToProtoError(err)}, nil
	}
	contactRef := strings.TrimSpace(req.GetContactRef())
	if contactRef == "" {
		return &waappv1.DeleteWAContactResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "contact_ref is required", false))}, nil
	}
	contactRefs := s.resolveContactActionRefs(ctx, accountID, contactRef)
	result, err := s.store.DeleteWAContact(ctx, accountID, contactRefs, s.clock.Now())
	if err != nil {
		return &waappv1.DeleteWAContactResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.DeleteWAContactResponse{Deleted: result.Deleted, DeletedMessageCount: int32(result.DeletedMessageCount)}, nil
}

func (s *Server) resolveContactActionRefs(ctx context.Context, accountID string, contactRef string) []string {
	contact, err := s.store.GetWAContactByRef(ctx, accountID, contactRef)
	if err != nil || contact.GetWaAccountId() != accountID {
		return contactActionRefs(contactRef, nil)
	}
	return contactActionRefs(contactRef, contact)
}

func (s *Server) resolveContactJIDs(ctx context.Context, accountID string, requested []string, limit int) ([]string, error) {
	limit = normalizeContactResolveLimit(limit)
	if len(requested) > 0 {
		return firstNStrings(normalizeContactUsyncJIDs(requested), limit), nil
	}
	contacts, _, err := s.store.ListWAContacts(ctx, accountID, "", limit)
	if err != nil {
		return nil, err
	}
	jids := []string{}
	for _, contact := range contacts {
		if !needsContactResolution(contact) {
			continue
		}
		jids = append(jids, contact.GetJid())
	}
	return normalizeContactUsyncJIDs(jids), nil
}

func needsContactResolution(contact *waappv1.WAContact) bool {
	if contact == nil || !strings.HasSuffix(contact.GetJid(), "@lid") {
		return false
	}
	return !contactUsyncHasDisplayIdentity(contact) || contact.GetProfilePictureId() == ""
}

func (s *Server) activeContactResolveLoginState(ctx context.Context, accountID string) (*waappv1.LoginState, error) {
	records, err := s.store.ListActiveLoginStates(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		loginState := record.LoginState
		if loginState.GetWaAccountId() == accountID && loginState.GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			return loginState, nil
		}
	}
	return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "active login state not found", false)
}

// contactResolverRunner 优先复用该账号已建立的长连接 runner,让 usync 走同一条 chatd 连接
// (每账号一条连接),避免另开并发 ACTIVE 连接触发服务端 <conflict type="replaced"> 自我顶替。
// 长连接尚未就绪时回退到独立 runner(短连接窗口),与 accountSettingsRunner 一致。
func (s *Server) contactResolverRunner(loginState *waappv1.LoginState) (ProtocolEngine, func(), error) {
	if s.longConnections != nil {
		if runner := s.longConnections.Runner(loginState); runner != nil {
			return runner, func() {}, nil
		}
	}
	return s.runner, func() {}, nil
}

func normalizeContactResolveLimit(limit int) int {
	if limit <= 0 {
		return defaultContactResolveLimit
	}
	if limit > maxContactUsyncBatchSize {
		return maxContactUsyncBatchSize
	}
	return limit
}

func firstNStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func contactKindStorageValue(contact *waappv1.WAContact) string {
	if contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED {
		return waappv1.WAContactKind_WA_CONTACT_KIND_USER.String()
	}
	return contact.GetKind().String()
}

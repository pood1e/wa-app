package account

import (
	"context"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

const defaultContactResolveLimit = 50

type waContactResolver interface {
	ResolveContacts(context.Context, wacore.EngineContactResolveInput) wacore.EngineContactResolveResult
}

func (s *contactHandler) ListWAContacts(ctx context.Context, req *waappv1.ListWAContactsRequest) (*waappv1.ListWAContactsResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.ListWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	contacts, nextCursor, err := s.store.ListWAContacts(ctx, accountID, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	if err := s.enrichContactPreviews(ctx, accountID, contacts); err != nil {
		return &waappv1.ListWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.ListWAContactsResponse{Contacts: contacts, NextCursor: nextCursor}, nil
}

func (s *contactHandler) ResolveWAContacts(ctx context.Context, req *waappv1.ResolveWAContactsRequest) (*waappv1.ResolveWAContactsResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	jids, err := s.resolveContactJIDs(ctx, accountID, req.GetJids(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ResolveWAContactsResponse{Error: shared.ToProtoError(err)}, nil
	}
	if len(jids) == 0 {
		return &waappv1.ResolveWAContactsResponse{}, nil
	}
	result := s.resolveAccountContacts(ctx, accountID, jids)
	if len(result.Contacts) > 0 {
		if err := s.store.SaveWAContacts(ctx, result.Contacts); err != nil {
			return &waappv1.ResolveWAContactsResponse{Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.ResolveWAContactsResponse{Contacts: result.Contacts, QueriedCount: int32(result.Queried), ResolvedCount: int32(result.Resolved), Error: shared.ToProtoError(result.Err)}, nil
}

func (s *contactHandler) resolveAccountContacts(ctx context.Context, accountID string, jids []string) wacore.EngineContactResolveResult {
	loginState, err := s.activeContactResolveLoginState(ctx, accountID)
	if err != nil {
		return wacore.EngineContactResolveResult{Err: err}
	}
	runner, err := s.longConnections.Acquire(ctx, loginState)
	if err != nil {
		return wacore.EngineContactResolveResult{Err: err}
	}
	resolver, ok := runner.(waContactResolver)
	if !ok {
		return wacore.EngineContactResolveResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA contact resolver is not configured", false)}
	}
	return resolver.ResolveContacts(ctx, wacore.EngineContactResolveInput{
		WAAccountID:          accountID,
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		AppVersion:           s.loginStateAppVersion(ctx, loginState),
		JIDs:                 jids,
		RemoteTimeout:        protocolcommunication.DefaultContactUsyncTimeout,
	})
}

func (s *contactHandler) DeleteWAContact(ctx context.Context, req *waappv1.DeleteWAContactRequest) (*waappv1.DeleteWAContactResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.DeleteWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	contactRef := strings.TrimSpace(req.GetContactRef())
	if contactRef == "" {
		return &waappv1.DeleteWAContactResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "contact_ref is required", false))}, nil
	}
	contactRefs := s.resolveContactActionRefs(ctx, accountID, contactRef)
	result, err := s.store.DeleteWAContact(ctx, accountID, contactRefs, s.clock.Now())
	if err != nil {
		return &waappv1.DeleteWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.DeleteWAContactResponse{Deleted: result.Deleted, DeletedMessageCount: int32(result.DeletedMessageCount)}, nil
}

func (s *serverCore) resolveContactActionRefs(ctx context.Context, accountID string, contactRef string) []string {
	contact, err := s.store.GetWAContactByRef(ctx, accountID, contactRef)
	if err != nil || contact.GetWaAccountId() != accountID {
		return contactActionRefs(contactRef, nil)
	}
	return contactActionRefs(contactRef, contact)
}

func (s *serverCore) resolveContactJIDs(ctx context.Context, accountID string, requested []string, limit int) ([]string, error) {
	limit = normalizeContactResolveLimit(limit)
	if len(requested) > 0 {
		return firstNStrings(protocolcommunication.NormalizeContactUsyncJIDs(requested), limit), nil
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
	return protocolcommunication.NormalizeContactUsyncJIDs(jids), nil
}

func needsContactResolution(contact *waappv1.WAContact) bool {
	if contact == nil || !strings.HasSuffix(contact.GetJid(), "@lid") {
		return false
	}
	return !protocolcommunication.ContactUsyncHasDisplayIdentity(contact) || contact.GetProfilePictureId() == ""
}

func (s *serverCore) activeContactResolveLoginState(ctx context.Context, accountID string) (*waappv1.LoginState, error) {
	records, err := s.store.ListActiveLoginStates(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		loginState := record
		if loginState.GetWaAccountId() == accountID && loginState.GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			return loginState, nil
		}
	}
	if latest, err := s.store.GetLatestAccountLoginState(ctx, accountID); err == nil && latest.GetLastError() != nil {
		return nil, shared.ErrorFromProto(latest.GetLastError())
	}
	return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "active login state not found", false)
}

func normalizeContactResolveLimit(limit int) int {
	if limit <= 0 {
		return defaultContactResolveLimit
	}
	if limit > protocolcommunication.MaxContactUsyncBatchSize {
		return protocolcommunication.MaxContactUsyncBatchSize
	}
	return limit
}

func firstNStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

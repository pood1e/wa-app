package account

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *messagingHandler) OpenMessageSession(ctx context.Context, req *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error) {

	account, profile, err := s.waAccountAndProfile(ctx, req.GetWaAccountId(), req.GetClientProfileId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	waID := wamodel.WAAccountID(account)
	if req.GetRegisteredIdentityId() == "" {
		return &waappv1.OpenMessageSessionResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registered_identity_id is required", false))}, nil
	}
	loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, req.GetRegisteredIdentityId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	if loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE || loginState.GetWaAccountId() != waID || loginState.GetClientProfileId() != req.GetClientProfileId() {
		return &waappv1.OpenMessageSessionResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "registered identity is not active for WA account profile", false))}, nil
	}
	now := s.clock.Now()
	session := &waappv1.MessageSession{MessageSessionId: s.ids.NewID("wasess_"), WaAccountId: waID, ClientProfileId: req.GetClientProfileId(), RegisteredIdentityId: req.GetRegisteredIdentityId(), ProtocolProfileId: shared.FirstNonEmpty(req.GetProtocolProfileId(), profile.GetProtocolProfileId()), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: timestamppb.New(now), LastSeenAt: timestamppb.New(now)}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	if err := s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute); err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = shared.ToProtoError(err)
		_ = s.store.SaveMessageSession(ctx, session)
		return &waappv1.OpenMessageSessionResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.OpenMessageSessionResponse{Session: session}, nil
}

func (s *messagingHandler) GetMessageSession(ctx context.Context, req *waappv1.GetMessageSessionRequest) (*waappv1.GetMessageSessionResponse, error) {

	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.GetMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetMessageSessionResponse{Session: session}, nil
}

func (s *messagingHandler) ReceiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest) (*waappv1.ReceiveMessageBatchResponse, error) {
	return s.ReceiveWithProtocol(ctx, req, nil)
}

func (s *serverCore) ReceiveWithProtocol(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest, runner wacore.Communication) (*waappv1.ReceiveMessageBatchResponse, error) {

	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Error: shared.ToProtoError(err)}, nil
	}
	if session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED || session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "message session is not open", false))}, nil
	}
	if runner == nil {
		login, lookupErr := s.store.GetLoginStateByRegisteredIdentity(ctx, session.GetRegisteredIdentityId())
		if lookupErr != nil {
			return &waappv1.ReceiveMessageBatchResponse{Error: shared.ToProtoError(lookupErr)}, nil
		}
		runner, err = s.longConnections.Acquire(ctx, login)
		if err != nil {
			return &waappv1.ReceiveMessageBatchResponse{Error: shared.ToProtoError(err)}, nil
		}
	}
	result := runner.ReceiveMessageBatch(ctx, wacore.EngineMessageInput{WAAccountID: session.GetWaAccountId(), ClientProfileID: session.GetClientProfileId(), RegisteredIdentityID: session.GetRegisteredIdentityId(), ProtocolProfileID: session.GetProtocolProfileId(), AppVersion: s.protocolIDAppVersion(ctx, session.GetProtocolProfileId()), MessageSessionID: session.GetMessageSessionId(), WaitTimeout: shared.DurationFromProto(req.GetWaitTimeout()), MaxMessages: int(req.GetMaxMessages())})
	if result.Err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = shared.ToProtoError(result.Err)
		_ = s.store.SaveMessageSession(ctx, session)
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(result.Err)}, nil
	}
	if err := s.saveInboundMessagesForSession(ctx, session, result.Messages); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	for _, msg := range result.OTPMessages {
		if err := s.store.SaveOTPMessage(ctx, msg); err != nil {
			return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
		}
	}
	if len(result.Contacts) > 0 {
		_ = s.store.SaveWAContacts(ctx, result.Contacts)
	}
	if err := s.store.SaveMessageReceipts(ctx, session.GetWaAccountId(), result.Receipts); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	if err := s.saveContactPresences(ctx, session, result.Presences); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	if result.AccountLogout == nil {
		s.flushDeliveryReceipts(ctx, session, runner)
	}
	now := s.clock.Now()
	session.LastSeenAt = timestamppb.New(now)
	loggedOut := result.AccountLogout
	if loggedOut != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
		session.LastError = shared.ToProtoError(protocolcommunication.AccountLoggedOutError(loggedOut.Reason))
	} else {
		_ = s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute)
	}
	if loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, session.GetRegisteredIdentityId()); loggedOut == nil && err == nil && loginState.GetWaAccountId() == session.GetWaAccountId() && loginState.GetClientProfileId() == session.GetClientProfileId() {
		if loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "login identity is not active", false))}, nil
		}
		if loginState.Audit == nil {
			loginState.Audit = &waappv1.AuditStamp{CreatedAt: timestamppb.New(now)}
		}
		loginState.LastVerifiedAt = timestamppb.New(now)
		loginState.LastError = nil

		loginState.Audit.UpdatedAt = timestamppb.New(now)
		if err := s.store.SaveLoginState(ctx, loginState); err != nil {
			return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
		}
	}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	if loggedOut != nil {
		s.AccountTransferredOut(ctx, &waappv1.LoginState{WaAccountId: session.GetWaAccountId(), RegisteredIdentityId: session.GetRegisteredIdentityId()}, protocolcommunication.AccountLoggedOutError(loggedOut.Reason))
	}
	return &waappv1.ReceiveMessageBatchResponse{Messages: result.Messages, Session: session}, nil
}

func (s *messagingHandler) ListAccountMessages(ctx context.Context, req *waappv1.ListAccountMessagesRequest) (*waappv1.ListAccountMessagesResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if strings.TrimSpace(req.GetContactRef()) == "" {
		return &waappv1.ListAccountMessagesResponse{}, nil
	}
	contactRefs := s.resolveContactActionRefs(ctx, accountID, req.GetContactRef())
	items, nextCursor, err := s.store.ListAccountMessages(ctx, accountID, contactRefs, req.GetCursor(), int(req.GetLimit()), req.GetIncludeSensitiveText())
	if err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if err := s.attachMessageReceipts(ctx, accountID, items); err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if err := s.attachMessageChanges(ctx, accountID, contactRefs, items, req.GetIncludeSensitiveText()); err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.ListAccountMessagesResponse{Messages: items, NextCursor: nextCursor}, nil
}

func (s *messagingHandler) GetLongConnectionStatus(ctx context.Context, req *waappv1.GetLongConnectionStatusRequest) (*waappv1.GetLongConnectionStatusResponse, error) {

	if req.GetWaAccountId() != "" {
		accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
		if err != nil {
			return &waappv1.GetLongConnectionStatusResponse{Error: shared.ToProtoError(err)}, nil
		}
		req.WaAccountId = accountID
	}
	if s.longConnections == nil {
		return &waappv1.GetLongConnectionStatusResponse{}, nil
	}
	return &waappv1.GetLongConnectionStatusResponse{Connections: s.longConnections.Snapshots(req)}, nil
}

func (s *messagingHandler) AcknowledgeMessage(ctx context.Context, req *waappv1.AcknowledgeMessageRequest) (*waappv1.AcknowledgeMessageResponse, error) {

	msg, err := s.store.GetInboundMessage(ctx, req.GetMessageId())
	if err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	if msg.GetMessageSessionId() != req.GetMessageSessionId() {
		return &waappv1.AcknowledgeMessageResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message does not belong to session", false))}, nil
	}
	msg.AckStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	if session, err := s.store.GetMessageSession(ctx, msg.GetMessageSessionId()); err == nil {
		err = s.saveInboundMessagesForSession(ctx, session, []*waappv1.InboundMessage{msg})
		if err != nil {
			return &waappv1.AcknowledgeMessageResponse{Error: shared.ToProtoError(err)}, nil
		}
	} else if err := s.store.SaveInboundMessages(ctx, []*waappv1.InboundMessage{msg}); err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.AcknowledgeMessageResponse{Message: msg}, nil
}

func (s *messagingHandler) CloseMessageSession(ctx context.Context, req *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error) {

	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
	session.ClosedAt = timestamppb.New(now)
	if err := s.runtime.CloseSessionLease(ctx, session.GetMessageSessionId()); err != nil {
		return &waappv1.CloseMessageSessionResponse{Session: session, Error: shared.ToProtoError(err)}, nil
	}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.CloseMessageSessionResponse{Session: session}, nil
}

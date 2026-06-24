package app

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) OpenMessageSession(ctx context.Context, req *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	account, profile, err := s.waAccountAndProfile(ctx, req.GetWaAccountId(), req.GetClientProfileId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	waID := waAccountID(account)
	if req.GetRegisteredIdentityId() == "" {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registered_identity_id is required", false))}, nil
	}
	loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, req.GetRegisteredIdentityId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	if loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE || loginState.GetWaAccountId() != waID || loginState.GetClientProfileId() != req.GetClientProfileId() {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "registered identity is not active for WA account profile", false))}, nil
	}
	now := s.clock.Now()
	session := &waappv1.MessageSession{MessageSessionId: s.ids.NewID("wasess_"), WaAccountId: waID, ClientProfileId: req.GetClientProfileId(), RegisteredIdentityId: req.GetRegisteredIdentityId(), ProtocolProfileId: firstNonEmpty(req.GetProtocolProfileId(), profile.GetProtocolProfileId()), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: timestamppb.New(now), LastSeenAt: timestamppb.New(now)}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	if err := s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute); err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = ToProtoError(err)
		_ = s.store.SaveMessageSession(ctx, session)
		return &waappv1.OpenMessageSessionResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	return &waappv1.OpenMessageSessionResponse{Session: session}, nil
}

func (s *Server) GetMessageSession(ctx context.Context, req *waappv1.GetMessageSessionRequest) (*waappv1.GetMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.GetMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetMessageSessionResponse{Session: session}, nil
}

func (s *Server) ReceiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest) (*waappv1.ReceiveMessageBatchResponse, error) {
	return s.receiveMessageBatch(ctx, req, s.runner)
}

func (s *Server) receiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest, runner ProtocolEngine) (*waappv1.ReceiveMessageBatchResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Error: ToProtoError(err)}, nil
	}
	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Error: ToProtoError(err)}, nil
	}
	if session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED || session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "message session is not open", false))}, nil
	}
	if runner == nil {
		runner = s.runner
	}
	result := runner.ReceiveMessageBatch(ctx, EngineMessageInput{WAAccountID: session.GetWaAccountId(), ClientProfileID: session.GetClientProfileId(), RegisteredIdentityID: session.GetRegisteredIdentityId(), ProtocolProfileID: session.GetProtocolProfileId(), AppVersion: s.protocolIDAppVersion(ctx, session.GetProtocolProfileId()), MessageSessionID: session.GetMessageSessionId(), WaitTimeout: durationFromProto(req.GetWaitTimeout()), MaxMessages: int(req.GetMaxMessages())})
	if result.Err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = ToProtoError(result.Err)
		_ = s.store.SaveMessageSession(ctx, session)
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(result.Err)}, nil
	}
	if err := s.saveInboundMessagesForSession(ctx, session, result.Messages); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	for _, msg := range result.OTPMessages {
		if err := s.store.SaveOTPMessage(ctx, msg); err != nil {
			return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(err)}, nil
		}
	}
	if len(result.Contacts) > 0 {
		_ = s.store.SaveWAContacts(ctx, result.Contacts)
	}
	now := s.clock.Now()
	session.LastSeenAt = timestamppb.New(now)
	loggedOut := result.AccountLogout
	if loggedOut != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
		session.LastError = ToProtoError(accountLoggedOutError(loggedOut.Reason))
	} else {
		_ = s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute)
	}
	if loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, session.GetRegisteredIdentityId()); err == nil && loginState.GetWaAccountId() == session.GetWaAccountId() && loginState.GetClientProfileId() == session.GetClientProfileId() {
		if loginState.Audit == nil {
			loginState.Audit = &waappv1.AuditStamp{CreatedAt: timestamppb.New(now)}
		}
		if loggedOut != nil {
			loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED
			loginState.LastError = ToProtoError(accountLoggedOutError(loggedOut.Reason))
		} else {
			loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
			loginState.LastVerifiedAt = timestamppb.New(now)
			loginState.LastError = nil
		}
		loginState.Audit.UpdatedAt = timestamppb.New(now)
		_ = s.store.SaveLoginState(ctx, loginState, "native-db:"+session.GetClientProfileId())
	}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	if loggedOut != nil {
		s.markWAAccountTransferredOut(ctx, session.GetWaAccountId())
		s.revokeLongConnection(session.GetRegisteredIdentityId(), accountLoggedOutError(loggedOut.Reason))
	}
	return &waappv1.ReceiveMessageBatchResponse{Messages: result.Messages, Session: session}, nil
}

func (s *Server) ListAccountMessages(ctx context.Context, req *waappv1.ListAccountMessagesRequest) (*waappv1.ListAccountMessagesResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: ToProtoError(err)}, nil
	}
	if strings.TrimSpace(req.GetContactRef()) == "" {
		return &waappv1.ListAccountMessagesResponse{}, nil
	}
	contactRefs := s.resolveContactActionRefs(ctx, accountID, req.GetContactRef())
	items, nextCursor, err := s.store.ListAccountMessages(ctx, accountID, contactRefs, req.GetCursor(), int(req.GetLimit()), req.GetIncludeSensitiveText())
	if err != nil {
		return &waappv1.ListAccountMessagesResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.ListAccountMessagesResponse{Messages: items, NextCursor: nextCursor}, nil
}

func (s *Server) GetLongConnectionStatus(ctx context.Context, req *waappv1.GetLongConnectionStatusRequest) (*waappv1.GetLongConnectionStatusResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetLongConnectionStatusResponse{Error: ToProtoError(err)}, nil
	}
	if req.GetWaAccountId() != "" {
		accountID, err := requireWAAccountID(req.GetWaAccountId())
		if err != nil {
			return &waappv1.GetLongConnectionStatusResponse{Error: ToProtoError(err)}, nil
		}
		req.WaAccountId = accountID
	}
	if s.longConnections == nil {
		return &waappv1.GetLongConnectionStatusResponse{}, nil
	}
	return &waappv1.GetLongConnectionStatusResponse{Connections: s.longConnections.Snapshots(req)}, nil
}

func (s *Server) AcknowledgeMessage(ctx context.Context, req *waappv1.AcknowledgeMessageRequest) (*waappv1.AcknowledgeMessageResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	msg, err := s.store.GetInboundMessage(ctx, req.GetMessageId())
	if err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	if msg.GetMessageSessionId() != req.GetMessageSessionId() {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message does not belong to session", false))}, nil
	}
	msg.AckStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	if session, err := s.store.GetMessageSession(ctx, msg.GetMessageSessionId()); err == nil {
		err = s.saveInboundMessagesForSession(ctx, session, []*waappv1.InboundMessage{msg})
		if err != nil {
			return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
		}
	} else if err := s.store.SaveInboundMessages(ctx, []*waappv1.InboundMessage{msg}); err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.AcknowledgeMessageResponse{Message: msg}, nil
}

func (s *Server) CloseMessageSession(ctx context.Context, req *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	session, err := s.store.GetMessageSession(ctx, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
	session.ClosedAt = timestamppb.New(now)
	if err := s.runtime.CloseSessionLease(ctx, session.GetMessageSessionId()); err != nil {
		return &waappv1.CloseMessageSessionResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.CloseMessageSessionResponse{Session: session}, nil
}

package account

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	protocolwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

type waMessageSender interface {
	SendMessage(context.Context, wacore.EngineMessageSendInput) wacore.EngineMessageSendResult
}

func (s *messagingHandler) SendTextMessage(ctx context.Context, req *waappv1.SendTextMessageRequest) (*waappv1.SendTextMessageResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	text := strings.TrimSpace(req.GetText().GetValue())
	if text == "" {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "text is required", false))}, nil
	}
	contactJID := s.textMessageContactJID(ctx, accountID, req.GetContactRef())
	if contactJID == "" {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "contact_ref is required", false))}, nil
	}
	loginState, err := s.activeContactResolveLoginState(ctx, accountID)
	if err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	body := &waE2E.Message{Conversation: proto.String(text)}
	if targetID := req.GetReplyToAccountMessageId(); targetID != "" {
		body, err = s.quotedTextMessage(ctx, accountID, contactJID, targetID, text)
		if err != nil {
			return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
		}
	}
	return s.sendAccountMessage(ctx, loginState, contactJID, body, req.GetClientMessageId()), nil
}

func (s *serverCore) sendAccountMessage(ctx context.Context, loginState *waappv1.LoginState, contactJID string, body *waE2E.Message, clientID string) *waappv1.SendTextMessageResponse {
	runner, err := s.longConnections.Acquire(ctx, loginState)
	if err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}
	}
	sender, ok := runner.(waMessageSender)
	if !ok {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA message sender is not configured", false))}
	}
	providerID := protocolcommunication.NewTextProviderMessageID(clientID)
	result := sender.SendMessage(ctx, wacore.EngineMessageSendInput{
		WAAccountID: loginState.GetWaAccountId(), ClientProfileID: loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(), AppVersion: s.loginStateAppVersion(ctx, loginState),
		ContactJID: contactJID, Message: body, ClientMessageID: providerID,
		RemoteTimeout: protocolcommunication.DefaultTextMessageSendTimeout,
	})
	if result.ProviderMessageID != "" {
		providerID = result.ProviderMessageID
	}
	if result.Err != nil {
		return &waappv1.SendTextMessageResponse{ProviderMessageId: providerID, SentAt: shared.ProtoTimestamp(result.SentAt), Error: shared.ToProtoError(result.Err)}
	}
	sentAt := result.SentAt
	if sentAt.IsZero() {
		sentAt = s.clock.Now()
	}
	ackStatus := result.AckStatus
	if ackStatus == waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_UNSPECIFIED {
		ackStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_PENDING
	}
	return &waappv1.SendTextMessageResponse{ProviderMessageId: providerID, SentAt: timestamppb.New(sentAt), Error: shared.ToProtoError(s.saveOutboundMessage(ctx, loginState, contactJID, providerID, body, sentAt, ackStatus))}
}

func (s *serverCore) textMessageContactJID(ctx context.Context, accountID string, contactRef string) string {
	contactRef = strings.TrimSpace(contactRef)
	if contactRef == "" {
		return ""
	}
	contact, err := s.store.GetWAContactByRef(ctx, accountID, contactRef)
	if err == nil && contact.GetWaAccountId() == accountID {
		if jid := wacore.NormalizeWAJID(contact.GetJid()); jid != "" {
			return jid
		}
		if number := strings.TrimSpace(contact.GetNumber()); number != "" {
			return wacore.NormalizeWAJID(number)
		}
	}
	return wacore.NormalizeWAJID(contactRef)
}

func (s *serverCore) saveOutboundMessage(ctx context.Context, loginState *waappv1.LoginState, contactJID string, providerID string, body *waE2E.Message, sentAt time.Time, ackStatus waappv1.MessageAckStatus) error {
	session, err := s.outboundMessageSession(ctx, loginState, sentAt)
	if err != nil {
		return err
	}
	text, content, kind := protocolcommunication.ProjectMessage(body, true)
	messageID := outboundMessageID(session.GetWaAccountId(), providerID)
	message := &waappv1.InboundMessage{
		MessageId:         messageID,
		MessageSessionId:  session.GetMessageSessionId(),
		Kind:              kind,
		EncryptionState:   waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED,
		AckStatus:         ackStatus,
		ContactRef:        contactJID,
		PayloadRef:        "outbound:" + providerID,
		ProviderMessageId: providerID,
		ProviderTimestamp: timestamppb.New(sentAt.UTC()),
		ReceivedAt:        timestamppb.New(sentAt.UTC()),
		DeleteStatus:      waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED,
		Direction:         waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND,
		Source:            waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LOCAL_SEND,
	}
	if err := s.saveInboundMessagesForSession(ctx, session, []*waappv1.InboundMessage{message}); err != nil {
		return err
	}
	return s.store.SaveDecryptedMessage(ctx, &waappv1.DecryptedMessage{
		DecryptedMessageId: "wadec_" + shared.StableID(messageID+":outbound"),
		MessageId:          messageID,
		Status:             waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED,
		PlaintextRef:       "outbound:" + messageID,
		PlaintextText:      protocolwire.SensitiveOutput(text, "outbound-message", true),
		DecryptedAt:        timestamppb.New(sentAt.UTC()),
		Content:            content,
	})
}

func (s *serverCore) outboundMessageSession(ctx context.Context, loginState *waappv1.LoginState, now time.Time) (*waappv1.MessageSession, error) {
	if s.longConnections != nil {
		if sessionID := s.longConnections.MessageSessionID(loginState); sessionID != "" {
			if session, err := s.store.GetMessageSession(ctx, sessionID); err == nil {
				return session, nil
			}
		}
	}
	profile, err := s.store.GetClientProfile(ctx, loginState.GetClientProfileId())
	if err != nil {
		return nil, err
	}
	session := &waappv1.MessageSession{
		MessageSessionId:     s.ids.NewID("wasess_"),
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
		ProtocolProfileId:    profile.GetProtocolProfileId(),
		Status:               waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED,
		OpenedAt:             timestamppb.New(now.UTC()),
		LastSeenAt:           timestamppb.New(now.UTC()),
		ClosedAt:             timestamppb.New(now.UTC()),
	}
	if err := s.store.SaveMessageSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func outboundMessageID(accountID string, providerID string) string {
	return "wamsg_" + shared.StableID(strings.Join([]string{accountID, providerID, "outbound"}, ":"))
}

package account

import (
	"context"
	"strings"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *messagingHandler) EditAccountMessage(ctx context.Context, req *waappv1.EditAccountMessageRequest) (*waappv1.SendTextMessageResponse, error) {
	text := strings.TrimSpace(req.GetText().GetValue())
	if text == "" {
		return messageActionError("text is required"), nil
	}
	target, login, err := s.messageChangeTarget(ctx, req.GetWaAccountId(), req.GetAccountMessageId())
	if err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	if target.GetDirection() != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND || target.GetContent().GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT {
		return messageActionError("only an outgoing text message can be edited"), nil
	}
	body := wire.BuildMessageEdit(messageChangeKey(target), text, s.clock.Now())
	return s.sendAccountMessage(ctx, login, target.GetContactRef(), body, req.GetClientMessageId()), nil
}

func (s *messagingHandler) ReactToAccountMessage(ctx context.Context, req *waappv1.ReactToAccountMessageRequest) (*waappv1.SendTextMessageResponse, error) {
	text := strings.TrimSpace(req.GetText())
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 32 || strings.ContainsAny(text, "\r\n\x00") {
		return messageActionError("reaction text is invalid"), nil
	}
	target, login, err := s.messageChangeTarget(ctx, req.GetWaAccountId(), req.GetAccountMessageId())
	if err != nil {
		return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(err)}, nil
	}
	body := wire.BuildMessageReaction(messageChangeKey(target), text, s.clock.Now())
	return s.sendAccountMessage(ctx, login, target.GetContactRef(), body, req.GetClientMessageId()), nil
}

func (s *serverCore) messageChangeTarget(ctx context.Context, accountID, messageID string) (*waappv1.AccountMessage, *waappv1.LoginState, error) {
	if _, err := wamodel.RequireWAAccountID(accountID); err != nil {
		return nil, nil, err
	}
	records, err := s.loadMessageActionRecords(ctx, accountID, []string{messageID})
	if err != nil {
		return nil, nil, err
	}
	record := records[0]
	decrypted, err := s.store.GetLatestDecryptedMessage(ctx, record.message.GetMessageId())
	if err != nil {
		return nil, nil, err
	}
	target := wamodel.NewAccountMessageFromInbound(accountID, record.message, decrypted, true)
	contacts := s.resolveContactActionRefs(ctx, accountID, target.GetContactRef())
	if err := s.attachMessageChanges(ctx, accountID, contacts, []*waappv1.AccountMessage{target}, true); err != nil {
		return nil, nil, err
	}
	if target.GetKind() != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE || target.GetProviderMessageId() == "" || target.GetRevokedAt() != nil || target.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME {
		return nil, nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message is unavailable", false)
	}
	login, err := s.activeContactResolveLoginState(ctx, accountID)
	return target, login, err
}

func (s *serverCore) quotedTextMessage(ctx context.Context, accountID, contactJID, targetID, text string) (*waE2E.Message, error) {
	target, _, err := s.messageChangeTarget(ctx, accountID, targetID)
	if err != nil {
		return nil, err
	}
	if target.GetContent().GetViewOnce() || target.GetContent().GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "quoted message must be an available text message", false)
	}
	if !containsContactRef(s.resolveContactActionRefs(ctx, accountID, contactJID), target.GetContactRef()) {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "quoted message belongs to another chat", false)
	}
	participant := target.GetSenderRef()
	if target.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND {
		account, err := s.GetWAAccountRecord(ctx, accountID)
		if err != nil {
			return nil, err
		}
		participant = strings.TrimPrefix(account.GetPhone().GetE164Number(), "+") + "@s.whatsapp.net"
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: &waE2E.ContextInfo{
		StanzaID: proto.String(target.GetProviderMessageId()), Participant: proto.String(participant),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(target.GetText().GetValue())},
	}}}, nil
}

func containsContactRef(refs []string, target string) bool {
	for _, ref := range refs {
		if ref == target {
			return true
		}
	}
	return false
}

func messageChangeKey(target *waappv1.AccountMessage) *waCommon.MessageKey {
	fromMe := target.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND
	key := &waCommon.MessageKey{RemoteJID: proto.String(target.GetContactRef()), FromMe: proto.Bool(fromMe), ID: proto.String(target.GetProviderMessageId())}
	if !fromMe && strings.HasSuffix(target.GetContactRef(), "@g.us") {
		participant, _ := types.ParseJID(target.GetSenderRef())
		key.Participant = proto.String(participant.ToNonAD().String())
	}
	return key
}

func messageActionError(message string) *waappv1.SendTextMessageResponse {
	return &waappv1.SendTextMessageResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, message, false))}
}

func (s *serverCore) revokeAccountMessages(ctx context.Context, accountID string, ids []string) (*waappv1.DeleteAccountMessagesResponse, error) {
	var changed int32
	for _, id := range normalizeActionMessageIDs(ids) {
		target, login, err := s.messageChangeTarget(ctx, accountID, id)
		if err != nil {
			return &waappv1.DeleteAccountMessagesResponse{UpdatedCount: changed, Error: shared.ToProtoError(err)}, nil
		}
		if target.GetDirection() != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND {
			return &waappv1.DeleteAccountMessagesResponse{UpdatedCount: changed, Error: messageActionError("only outgoing messages can be revoked").GetError()}, nil
		}
		body := wire.BuildMessageRevoke(messageChangeKey(target))
		response := s.sendAccountMessage(ctx, login, target.GetContactRef(), body, "")
		if response.GetError() != nil {
			return &waappv1.DeleteAccountMessagesResponse{UpdatedCount: changed, Error: response.GetError()}, nil
		}
		changed++
	}
	return &waappv1.DeleteAccountMessagesResponse{UpdatedCount: changed}, nil
}

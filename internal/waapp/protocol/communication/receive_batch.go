package communication

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

var errReceiveWindowComplete = errors.New("chatd receive window complete")

func (s *ChatdSession) ReceiveBatch(ctx context.Context, input wacore.EngineMessageInput, clock shared.Clock) ([]*waappv1.InboundMessage, []pwire.ChatdEncPayload, []*waappv1.OtpMessage, ChatdSessionUpdate, error) {
	if s == nil {
		return nil, nil, nil, ChatdSessionUpdate{}, fmt.Errorf("chatd session is not open")
	}
	update := s.Update()
	maxMessages := input.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 10
	}
	deadline := pwire.WaitDeadline(input.WaitTimeout)
	items := []ChatdReceivedItem{}
	for len(items) < maxMessages && time.Now().Before(deadline) {
		nextUpdate, nextItems, err := s.receiveBatchNode(ctx, input, update, deadline, clock)
		update = nextUpdate
		if err != nil {
			if errors.Is(err, errReceiveWindowComplete) {
				break
			}
			if len(items) > 0 {
				break
			}
			return nil, nil, nil, update, err
		}
		before := len(items)
		items = AppendReceivedItems(items, nextItems, maxMessages)
		if (len(items) > before || len(update.Receipts) > 0 || len(update.Presences) > 0 || len(update.SignalRetries) > 0) && len(items) < maxMessages {
			deadline = MinTime(deadline, time.Now().Add(DefaultChatdPostMessageDrainWindow))
		}
	}
	messages, payloads, otps := SplitReceivedItems(items)
	return messages, payloads, otps, update, nil
}

func (s *ChatdSession) receiveBatchNode(ctx context.Context, input wacore.EngineMessageInput, update ChatdSessionUpdate, deadline time.Time, clock shared.Clock) (ChatdSessionUpdate, []ChatdReceivedItem, error) {
	finish, err := s.bindDeadline(ctx, deadline)
	if err != nil {
		return update, nil, err
	}
	defer finish()
	node, err := s.Transport.ReadNode()
	if err != nil {
		if ctx.Err() != nil {
			return update, nil, ctx.Err()
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return update, nil, errReceiveWindowComplete
		}
		return update, nil, pwire.ChatdPhase("chatd frame read", err)
	}
	return s.ConsumeIncomingNode(input, node, update, clock.Now())
}

func (s *ChatdSession) ConsumeIncomingNode(input wacore.EngineMessageInput, node pwire.ChatdNode, update ChatdSessionUpdate, now time.Time) (ChatdSessionUpdate, []ChatdReceivedItem, error) {
	if node.Tag == "xmlstreamend" {
		return update, nil, s.Transport.fail(pwire.NewChatdError("server closed xml stream"))
	}
	if pwire.IsChatdTerminalNode(node) {
		summary := pwire.ControlNodeSummary(node)
		if ChatdTerminalNodeAccountTakeover(node) {
			return update, nil, s.Transport.fail(AccountTakenOverError(summary))
		}
		if err := chatdAuthenticationFailure(node); err != nil {
			return update, nil, s.Transport.fail(err)
		}
		return update, nil, s.Transport.fail(pwire.NewChatdError("server sent %s", summary))
	}
	oldRegistrationOTP, isOldRegistrationNode, oldRegistrationValid := OldRegistrationOTPFromChatdNode(node, s.DeviceID, now)
	ackAttrs := map[string]string(nil)
	if isOldRegistrationNode && s.DeviceID != "" {
		ackAttrs = map[string]string{"device_id": s.DeviceID}
	}
	_, signalRetry := s.parseSignalRetry(node)
	if ack, ok := pwire.BuildAckForNodeWithAttrs(node, ackAttrs); ok && !signalRetry {
		if err := s.Transport.SendNode(ack); err != nil {
			return update, nil, pwire.ChatdPhase("chatd ack write", err)
		}
	}
	if nextRouting := pwire.RoutingInfoFromNode(node); nextRouting != "" {
		update.RoutingInfo = nextRouting
	}
	update.ContactHints = pwire.DedupeWAContactHints(append(update.ContactHints, pwire.ContactHintsFromChatdNode(node)...))
	update.PrivacyTokens = DedupePrivacyTokenUpdates(append(update.PrivacyTokens, PrivacyTokenUpdatesFromChatdNode(node)...))
	if nonce := canonicalNonceFromNode(node); nonce != "" {
		s.canonicalNonce = nonce
	}
	update.Receipts = append(update.Receipts, s.messageReceipts(node)...)
	if signalRetry {
		update.SignalRetries = append(update.SignalRetries, node)
	}
	if presence := s.contactPresence(node, now); presence != nil {
		update.Presences = append(update.Presences, presence)
	}
	if logout, ok := DeviceLogoutFromChatdNode(node); ok {
		update.AccountLogout = &logout
	}
	items := []ChatdReceivedItem{}
	if oldRegistrationValid {
		if otp := OldRegistrationOTPMessage(input, node, oldRegistrationOTP, now); otp != nil {
			items = append(items, ChatdReceivedItem{Otp: otp})
		}
	}
	if input.MessageSessionID == "" {
		return update, items, nil
	}
	encs := pwire.IterEncPayloads(node)
	if len(encs) == 0 {
		return update, items, nil
	}
	source, ok := s.messageSource(node)
	if !ok || source.sender.IsEmpty() {
		return update, items, nil
	}
	for _, enc := range encs {
		payload := enc
		payload.Contact = source.chat.String()
		payload.Sender = source.sender.String()
		if source.senderAlt.Server == types.HiddenUserServer {
			payload.Sender = source.senderAlt.String()
		}
		payloadRef := pwire.PayloadRefForEnc(input.WAAccountID, payload.Payload)
		message := &waappv1.InboundMessage{MessageId: InboundMessageID(input.WAAccountID, payload.StanzaID, node.Tag, payload.Sender, payload.Path+":"+pwire.HexKey(payload.Payload)), MessageSessionId: input.MessageSessionID, Kind: InboundKind(node.Tag), EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED, AckStatus: AckStatusForNode(node), ContactRef: payload.Contact, SenderRef: payload.Sender, PayloadRef: payloadRef, ProviderMessageId: payload.StanzaID, ProviderTimestamp: ChatdProviderTimestamp(payload.StanzaTimestamp), DeleteStatus: waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED, ReceivedAt: timestamppb.New(now)}
		message.ContactRef = source.chat.String()
		if source.fromMe {
			message.Direction = waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND
		}
		items = append(items, ChatdReceivedItem{Message: message, Payload: &payload})
	}
	return update, items, nil
}

func AppendReceivedItems(dst []ChatdReceivedItem, src []ChatdReceivedItem, limit int) []ChatdReceivedItem {
	if limit <= 0 {
		return append(dst, src...)
	}
	for _, item := range src {
		if len(dst) >= limit {
			return dst
		}
		dst = append(dst, item)
	}
	return dst
}

func SplitReceivedItems(items []ChatdReceivedItem) ([]*waappv1.InboundMessage, []pwire.ChatdEncPayload, []*waappv1.OtpMessage) {
	messages := make([]*waappv1.InboundMessage, 0, len(items))
	payloads := []pwire.ChatdEncPayload{}
	otps := []*waappv1.OtpMessage{}
	for _, item := range items {
		if item.Otp != nil {
			otps = append(otps, item.Otp)
		}
		if item.Message != nil {
			messages = append(messages, item.Message)
			if item.Payload != nil {
				payloads = append(payloads, *item.Payload)
			}
		}
	}
	return messages, payloads, otps
}

func ChatdProviderTimestamp(value string) *timestamppb.Timestamp {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	stamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || stamp <= 0 {
		return nil
	}
	if stamp > 1_000_000_000_000 {
		return timestamppb.New(time.UnixMilli(stamp).UTC())
	}
	return timestamppb.New(time.Unix(stamp, 0).UTC())
}

func InboundMessageID(accountID string, stanzaID string, tag string, sender string, fingerprint string) string {
	return "wamsg_" + shared.StableID(strings.Join([]string{accountID, stanzaID, tag, sender, fingerprint}, ":"))
}

func (c *ChatdClient) CheckLoginState(ctx context.Context, state pstate.NativeState, input wacore.EngineLoginCheckInput, appVersion string) (ChatdSessionUpdate, error) {
	session, err := c.OpenSession(ctx, state, input.RegisteredIdentityID, PassiveLoginCheckPayload, appVersion)
	if err != nil {
		return ChatdSessionUpdate{}, err
	}
	defer session.Close()
	return session.Update(), nil
}

func EnsureChatStatic(key pwire.NativeCurveKeyPair) pwire.NativeCurveKeyPair {
	if key.Private != "" && key.Public != "" {
		return key
	}
	newKey, err := pwire.NewNativeCurveKeyPair()
	if err != nil {
		return key
	}
	return newKey
}

func MinTime(left time.Time, right time.Time) time.Time {
	if right.IsZero() || left.Before(right) {
		return left
	}
	return right
}

func MinDuration(left time.Duration, right time.Duration) time.Duration {
	if right <= 0 || left < right {
		return left
	}
	return right
}

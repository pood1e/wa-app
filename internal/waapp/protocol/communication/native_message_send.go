package communication

import (
	"context"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const DefaultTextMessageSendTimeout = 20 * time.Second

const TextMessageAckTimeoutMessage = "WA text message send acknowledgement timed out"

type ChatdMessageSendResult struct {
	wacore.EngineMessageSendResult
	Items  []ChatdReceivedItem
	Update ChatdSessionUpdate
}

func (e *messagingService) SendMessage(ctx context.Context, input wacore.EngineMessageSendInput) wacore.EngineMessageSendResult {
	if e == nil {
		return wacore.EngineMessageSendResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageSendResult{Err: err}
	}
	state.EnsureMaps()
	state.ChatStatic = EnsureChatStatic(state.ChatStatic)
	proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageSendResult{Err: err}
	}
	timeout := TextMessageSendTimeout(input.RemoteTimeout)
	operationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := NewChatdClient(ChatdConfigForState(proxyURL, state, timeout))
	session, err := client.OpenSession(operationCtx, state, input.RegisteredIdentityID, DefaultLoginPayload, input.AppVersion)
	if err != nil {
		return wacore.EngineMessageSendResult{Err: ChatdReceiveError(err)}
	}
	defer session.Close()
	receiveInput := wacore.EngineMessageInput{WAAccountID: input.WAAccountID, ClientProfileID: input.ClientProfileID, RegisteredIdentityID: input.RegisteredIdentityID, AppVersion: input.AppVersion}
	result := e.SendMessageOnSession(operationCtx, session, &state, input, receiveInput, timeout)
	if err := e.ApplyMessageSendUpdate(operationCtx, input.ClientProfileID, &state, receiveInput, result.Items, result.Update); err != nil && result.Err == nil {
		result.Err = err
	}
	return result.EngineMessageSendResult
}

func (e *Connection) SendMessage(ctx context.Context, input wacore.EngineMessageSendInput) wacore.EngineMessageSendResult {
	ctx, finish := e.operationContext(ctx)
	defer finish()
	if err := e.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineMessageSendResult{Err: err}
	}
	defer e.UnlockInteractiveIQLocked()
	if e.Closed {
		return wacore.EngineMessageSendResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageSendResult{Err: err}
	}
	state.EnsureMaps()
	state.ChatStatic = EnsureChatStatic(state.ChatStatic)
	messageInput := e.MessageReceiveInput(input)
	session, err := e.EnsureSessionForIQLocked(ctx, messageInput, state)
	if err != nil {
		e.closeFailedSessionLocked()
		return wacore.EngineMessageSendResult{Err: ChatdReceiveError(err)}
	}
	timeout := ContextBoundTimeout(ctx, TextMessageSendTimeout(input.RemoteTimeout))
	result := e.Engine.SendMessageOnSession(ctx, session, &state, input, messageInput, timeout)
	e.BufferPendingLocked(result.Items, result.Update)
	if err := e.Engine.ApplyMessageSendUpdate(ctx, input.ClientProfileID, &state, messageInput, result.Items, result.Update); err != nil && result.Err == nil {
		result.Err = err
	}
	if result.Err != nil {
		e.closeFailedSessionLocked()
	}
	return result.EngineMessageSendResult
}

func (e *messagingService) SendMessageOnSession(ctx context.Context, session *ChatdSession, state *pstate.NativeState, input wacore.EngineMessageSendInput, receiveInput wacore.EngineMessageInput, timeout time.Duration) ChatdMessageSendResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerID := NewTextProviderMessageID(input.ClientMessageID)
	sentAt := e.clock.Now()
	result := ChatdMessageSendResult{EngineMessageSendResult: wacore.EngineMessageSendResult{ProviderMessageID: providerID, SentAt: sentAt}}
	preparation := messagePreparation{session: session, current: state, input: receiveInput, timeout: timeout, update: session.Update()}
	target := wacore.NormalizeWAJID(input.ContactJID)
	if input.Message == nil {
		result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message body is required", false)
		return result
	}
	if previous := findSentMessage(state, target, providerID); previous.GetAcknowledged() {
		if !sameSentMessage(previous.GetMessage(), input.Message) {
			result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "message id is already used by another message", false)
			return result
		}
		result.SentAt = time.Unix(previous.GetTimestamp(), 0)
		result.AckStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
		return result
	}
	var node pwire.ChatdNode
	var err error
	if strings.HasSuffix(target, "@g.us") {
		node, err = preparation.groupMessage(ctx, target, providerID, input.Message)
	} else {
		node, err = preparation.directMessage(ctx, target, providerID, input.Message)
	}
	result.Items, result.Update = preparation.items, preparation.update
	if err != nil {
		result.Err = err
		return result
	}
	applyMessageNodeAttributes(&node, input.Message)
	record, err := rememberSentMessage(state, input.Message, node, sentAt)
	if err != nil {
		result.Err = err
		return result
	}
	ApplyChatdSessionUpdateState(state, session.Update())
	if err := e.SaveState(ctx, input.ClientProfileID, *state); err != nil {
		result.Err = err
		return result
	}
	items, update, err := session.SendMessageWithAck(ctx, receiveInput, node, providerID, timeout)
	result.Items = append(result.Items, items...)
	result.Update = MergeChatdSessionUpdate(result.Update, update)
	if err != nil {
		result.Err = ChatdSendError(err)
		return result
	}
	result.AckStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	record.Acknowledged = true
	if err := e.SaveState(ctx, input.ClientProfileID, *state); err != nil {
		result.Err = err
	}
	return result
}

func (e *messagingService) ApplyMessageSendUpdate(ctx context.Context, clientProfileID string, state *pstate.NativeState, input wacore.EngineMessageInput, items []ChatdReceivedItem, update ChatdSessionUpdate) error {
	if state == nil {
		return nil
	}
	_, payloads, _ := SplitReceivedItems(items)
	if !ApplyChatdReceiveState(state, input, payloads, update) {
		return nil
	}
	return e.SaveState(ctx, clientProfileID, *state)
}

func (e *Connection) MessageReceiveInput(input wacore.EngineMessageSendInput) wacore.EngineMessageInput {
	messageInput := e.Input
	messageInput.WAAccountID = shared.FirstNonEmpty(messageInput.WAAccountID, input.WAAccountID)
	messageInput.ClientProfileID = shared.FirstNonEmpty(messageInput.ClientProfileID, input.ClientProfileID)
	messageInput.RegisteredIdentityID = shared.FirstNonEmpty(messageInput.RegisteredIdentityID, input.RegisteredIdentityID)
	messageInput.AppVersion = shared.FirstNonEmpty(messageInput.AppVersion, input.AppVersion)
	return messageInput
}

func ExactSignalSession(sessions map[string]pstate.NativeSignalSession, contactJID string) (string, pstate.NativeSignalSession, bool) {
	for _, candidate := range shared.UniqueNonEmptyStrings(contactJID, wacore.NormalizeWAJID(contactJID)) {
		key := SignalSenderKey(candidate)
		if session, ok := sessions[key]; ok {
			return key, session, true
		}
	}
	return "", pstate.NativeSignalSession{}, false
}

func EnsureOutboundSignalChain(session *pstate.NativeSignalSession) error {
	if session.SenderChain != nil && session.SenderChain.ChainKey != "" && session.SenderRatchetPrivate != "" {
		return nil
	}
	if session.RootKey == "" {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA text send requires a learned Signal root key", false)
	}
	remoteRatchet, err := LatestReceiverRatchetKey(*session)
	if err != nil {
		return err
	}
	rootKey, err := pwire.DecodeB64Any(session.RootKey)
	if err != nil {
		return err
	}
	localRatchet, err := pwire.NewNativeCurveKeyPair()
	if err != nil {
		return err
	}
	localPrivate, err := localRatchet.PrivateBytes()
	if err != nil {
		return err
	}
	localPublic, err := localRatchet.PublicBytes()
	if err != nil {
		return err
	}
	nextRoot, chainKey, err := RootRatchet(rootKey, remoteRatchet, localPrivate)
	if err != nil {
		return err
	}
	session.RootKey = pwire.B64u(nextRoot)
	session.SenderRatchetPrivate = pwire.B64u(localPrivate)
	session.SenderRatchetPublic = pwire.B64u(localPublic)
	session.SenderChain = &pstate.NativeSenderChain{RatchetKey: pwire.B64u(localPublic), ChainKey: pwire.B64u(chainKey)}
	return nil
}

func LatestReceiverRatchetKey(session pstate.NativeSignalSession) ([]byte, error) {
	if len(session.ReceiverChains) == 0 {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA text send requires a learned receiver chain", false)
	}
	keys := make([]string, 0, len(session.ReceiverChains))
	for key := range session.ReceiverChains {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var selected pstate.NativeReceiverChain
	selectedOK := false
	for _, key := range keys {
		chain := session.ReceiverChains[key]
		if !selectedOK || chain.Index >= selected.Index {
			selected = chain
			selectedOK = true
		}
	}
	if selected.RatchetKey != "" {
		return pwire.DecodeB64Any(selected.RatchetKey)
	}
	for _, key := range keys {
		if raw, err := hex.DecodeString(key); err == nil && len(raw) > 0 {
			return raw, nil
		}
	}
	return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA text send receiver ratchet is unavailable", false)
}

package communication

import (
	"bytes"
	"context"
	"regexp"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

var ChatdNodeTokenErrorPattern = regexp.MustCompile(`(?i)(readstring could not match token|invalid list-size token)\s+([0-9]{1,3})`)

// messagingService owns inbound batch receive, decryption, text send and receipts.
type messagingService struct{ *engineCore }

// contactsService owns usync contact resolution and contact profile pictures.
type contactsService struct{ *engineCore }

// accountSettingsService owns 2FA / email / profile-name / picture operations.
type accountSettingsService struct{ *engineCore }

func (e *loginService) CheckLoginState(ctx context.Context, input wacore.EngineLoginCheckInput) wacore.EngineLoginCheckResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineLoginCheckResult{Status: LoginCheckStatusForError(err), Err: err}
	}
	proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNSPECIFIED, Err: err}
	}
	timeout := DefaultChatdReadWindow
	if input.RemoteTimeout > 0 {
		timeout = input.RemoteTimeout
	}
	client := NewChatdClient(ChatdConfigForState(proxyURL, state, timeout))
	update, err := client.CheckLoginState(ctx, state, input, input.AppVersion)
	if err != nil {
		err = e.saveLoginRejection(ctx, input.ClientProfileID, state, err)
	}
	if ApplyChatdSessionUpdateState(&state, update) {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	if err != nil {
		status := LoginCheckStatusForError(err)
		if IsAuthenticationFailure(err) {
			return wacore.EngineLoginCheckResult{Status: status, Err: err}
		}
		message := ChatdFailureMessage("login state remote check failed", err)
		return wacore.EngineLoginCheckResult{Status: status, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, status == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE)}
	}
	return wacore.EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE}
}

func LoginCheckStatusForError(err error) waappv1.LoginStateCheckStatus {
	if err == nil {
		return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE
	}
	if IsAuthenticationFailure(err) || IsAccountTakeoverError(err) {
		return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID
	}
	return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE
}

func (e *messagingService) ReceiveMessageBatch(ctx context.Context, input wacore.EngineMessageInput) wacore.EngineMessageBatchResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageBatchResult{Err: err}
	}
	state.EnsureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageBatchResult{Err: err}
	}
	client := NewChatdClient(ChatdConfigForState(proxyURL, state, 0))
	messages, payloads, otps, update, err := client.ReceiveBatch(ctx, state, input, input.AppVersion, e.clock)
	if err != nil {
		return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
	}
	if ApplyChatdReceiveState(&state, input, payloads, update) {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	return wacore.EngineMessageBatchResult{Messages: messages, Contacts: wamodel.ContactsFromContactHints(input.WAAccountID, nil, update.ContactHints, e.clock.Now()), OTPMessages: otps, AccountLogout: AccountLogoutFromUpdate(update.AccountLogout), Receipts: update.Receipts, Presences: update.Presences}
}

func ApplyChatdReceiveState(state *pstate.NativeState, input wacore.EngineMessageInput, payloads []pwire.ChatdEncPayload, update ChatdSessionUpdate) bool {
	if state == nil {
		return false
	}
	changed := false
	state.EnsureMaps()
	for _, payload := range payloads {
		ref := pwire.PayloadRefForEnc(input.WAAccountID, payload.Payload)
		state.MessagePayloads[ref] = pstate.NativeMessagePayload{
			Contact:             payload.Contact,
			Sender:              payload.Sender,
			ContactPN:           payload.ContactPN,
			SenderPN:            payload.SenderPN,
			NotifyName:          payload.NotifyName,
			ParticipantUsername: payload.ParticipantUsername,
			ContactHints:        pwire.DedupeWAContactHints(payload.ContactHints),
			EncType:             payload.EncType,
			Path:                payload.Path,
			Payload:             pwire.B64u(payload.Payload),
			ProviderMessageID:   payload.StanzaID,
			ProviderTimestamp:   shared.TimeFromProto(ChatdProviderTimestamp(payload.StanzaTimestamp)).Unix(),
			RetryCount:          payload.RetryCount,
		}
		changed = true
	}
	if ApplyChatdSessionUpdateState(state, update) {
		changed = true
	}
	return changed
}

func ApplyChatdSessionUpdateState(state *pstate.NativeState, update ChatdSessionUpdate) bool {
	state.EnsureMaps()
	changed := queueSignalRetries(state, update.SignalRetries)
	if len(update.ContactHints) > 0 {
		state.ContactHints = pwire.DedupeWAContactHints(append(state.ContactHints, update.ContactHints...))
		changed = true
	}
	if ApplyChatdConnectionState(state, update) {
		changed = true
	}
	if ApplyPrivacyTokenUpdates(state, update.PrivacyTokens) {
		changed = true
	}
	return changed
}

func MergeChatdSessionUpdate(current ChatdSessionUpdate, next ChatdSessionUpdate) ChatdSessionUpdate {
	if next.RoutingInfo != "" {
		current.RoutingInfo = next.RoutingInfo
	}
	if next.Endpoint.Host != "" {
		current.Endpoint = next.Endpoint
	}
	if next.ServerStaticPublic != "" {
		current.ServerStaticPublic = next.ServerStaticPublic
		current.ServerStaticPQPublic = bytes.Clone(next.ServerStaticPQPublic)
	}
	if len(next.ContactHints) > 0 {
		current.ContactHints = append(current.ContactHints, next.ContactHints...)
	}
	if len(next.PrivacyTokens) > 0 {
		current.PrivacyTokens = DedupePrivacyTokenUpdates(append(current.PrivacyTokens, next.PrivacyTokens...))
	}
	if next.AccountLogout != nil {
		current.AccountLogout = next.AccountLogout
	}
	current.Receipts = append(current.Receipts, next.Receipts...)
	current.Presences = append(current.Presences, next.Presences...)
	current.SignalRetries = append(current.SignalRetries, next.SignalRetries...)
	return current
}

func HasChatdSessionUpdate(update ChatdSessionUpdate) bool {
	return update.RoutingInfo != "" || update.Endpoint.Host != "" || update.ServerStaticPublic != "" || len(update.ContactHints) > 0 || len(update.PrivacyTokens) > 0 || update.AccountLogout != nil || len(update.Receipts) > 0 || len(update.Presences) > 0 || len(update.SignalRetries) > 0
}

func ApplyChatdConnectionState(state *pstate.NativeState, update ChatdSessionUpdate) bool {
	if state == nil {
		return false
	}
	if state.ChatConnection == nil && (update.Endpoint.Host != "" || update.ServerStaticPublic != "") {
		state.ChatConnection = new(protocolv1.ChatConnectionState)
	}
	changed := false
	if update.RoutingInfo != "" && state.ChatRoutingInfo != update.RoutingInfo {
		state.ChatRoutingInfo = update.RoutingInfo
		changed = true
	}
	if update.Endpoint.Host != "" && (state.ChatConnection.GetLastHost() != update.Endpoint.Host || int(state.ChatConnection.GetLastPort()) != update.Endpoint.Port) {
		state.ChatConnection.LastHost = update.Endpoint.Host
		state.ChatConnection.LastPort = int32(update.Endpoint.Port)
		changed = true
	}
	if update.ServerStaticPublic != "" && (state.ChatConnection.GetServerStaticPublic() != update.ServerStaticPublic ||
		!bytes.Equal(state.ChatConnection.GetServerStaticPqPublic(), update.ServerStaticPQPublic)) {
		state.ChatConnection.ServerStaticPublic = update.ServerStaticPublic
		state.ChatConnection.ServerStaticPqPublic = bytes.Clone(update.ServerStaticPQPublic)
		changed = true
	}
	return changed
}

// chatdFailureMessage appends any redacted chatd failure snippet to base.
func ChatdFailureMessage(base string, err error) string {
	if snippet := ChatdSafeFailureMessage(err); snippet != "" {
		return base + ": " + snippet
	}
	return base
}

func ChatdReceiveError(err error) error {
	if IsAuthenticationFailure(err) {
		return err
	}
	message := ChatdFailureMessage("native chatd receive failed", err)

	if IsAccountTakeoverError(err) {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, ChatdAccountTakeoverMarker+" "+message, false)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, true)
}

func ChatdSafeFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	phase := ChatdFailurePhase(text)
	reason := ChatdFailureReason(text)
	switch {
	case phase != "" && reason != "":
		return phase + ": " + reason
	case phase != "":
		return phase
	default:
		return pstate.SafeResponseSnippet(pstate.UpstreamFailureMessage(nil, err))
	}
}

func ChatdFailurePhase(text string) string {
	for _, phase := range []string{
		"prepare chatd static identity",
		"resolve chatd login identity",
		"decode chatd routing info",
		"chatd dial",
		"chatd encode routing info",
		"chatd write routing info",
		"chatd write noise prologue",
		"chatd flush noise prologue",
		"chatd generate ephemeral",
		"chatd write client hello",
		"chatd read server hello",
		"chatd parse server hello",
		"chatd mix ee",
		"chatd decrypt server static",
		"chatd mix es",
		"chatd decrypt server payload",
		"chatd encrypt client static",
		"chatd mix se",
		"chatd encrypt login payload",
		"chatd write client finish",
		"chatd noise handshake",
		"chatd ping write",
		"chatd frame read",
		"chatd ack write",
		"chatd iq write",
		"chatd iq read",
		"chatd message write",
		"chatd message read",
	} {
		if strings.Contains(text, phase) {
			return phase + " failed"
		}
	}
	return ""
}

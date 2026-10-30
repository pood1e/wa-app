package communication

import (
	"context"
	"encoding/base64"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"time"

	"go.mau.fi/whatsmeow/types"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (e *messagingService) decryptMessagePayload(ctx context.Context, input wacore.EngineDecryptInput, current *state.NativeState, payload state.NativeMessagePayload) ([]byte, error) {
	raw, err := e.decryptWithState(ctx, input, current, payload)
	if err == nil || !retryableDecryptionFailure(err) {
		return raw, err
	}
	profiles, historyErr := e.stateStore.ListRetiredProtocolProfiles(ctx, input.ClientProfileID)
	if historyErr != nil {
		return nil, historyErr
	}
	for _, id := range profiles {
		previous, loadErr := e.loadState(ctx, id)
		if loadErr != nil {
			return nil, loadErr
		}
		previousInput := input
		previousInput.ClientProfileID = id
		raw, previousErr := e.decryptWithState(ctx, previousInput, &previous, payload)
		if previousErr != nil {
			if retryableDecryptionFailure(previousErr) {
				continue
			}
			return nil, previousErr
		}
		// Delayed ciphertext advances its original ratchet, never the active identity's keys.
		if input.SessionCommitPolicy == waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE {
			current.MessagePlainRef[MessagePlaintextRef(input, payload)] = base64.StdEncoding.EncodeToString(raw)
			completeSignalRetry(current, payload, raw)
			if err := e.SaveState(ctx, input.ClientProfileID, *current); err != nil {
				return nil, err
			}
		}
		return raw, nil
	}
	return nil, err
}

func (e *messagingService) decryptWithState(ctx context.Context, input wacore.EngineDecryptInput, current *state.NativeState, payload state.NativeMessagePayload) ([]byte, error) {
	ref := MessagePlaintextRef(input, payload)
	raw, cached, err := cachedMessagePlaintext(current, input, ref)
	if err != nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "saved message plaintext is invalid", false)
	}
	if cached {
		return raw, nil
	}
	commit := input.SessionCommitPolicy == waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE
	output, err := DecryptNativeSignalPayload(current, payload, commit)
	if err != nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "native Signal message decryption failed", true)
	}
	raw = output.Plaintext
	if commit {
		if err := applySignalControlMessage(ctx, current, payload.Sender, raw); err != nil {
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "group sender-key distribution is invalid", false)
		}
		if ownProtocolSender(current, payload.Sender) {
			_ = ApplyNativeAppStateKeys(current, raw)
		}
		current.EnsureMaps()
		current.MessagePlainRef[ref] = base64.StdEncoding.EncodeToString(raw)
		completeSignalRetry(current, payload, raw)
		if err := e.SaveState(ctx, input.ClientProfileID, *current); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

func completeSignalRetry(current *state.NativeState, payload state.NativeMessagePayload, raw []byte) {
	if payload.ProviderMessageID == "" {
		return
	}
	message, err := DecodeSignalMessage(raw)
	if err != nil {
		return
	}
	_, content, kind := ProjectMessage(message, false)
	if kind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE && content.GetChange() == nil {
		return
	}
	key := protocolRetryKey(current, payload.Contact, payload.Sender, payload.ProviderMessageID)
	record := current.SignalRetries[key]
	if record == nil {
		record = &protocolv1.SignalRetryState{}
		current.SignalRetries[key] = record
	}
	record.Completed = true
	record.PendingReceipt = nil
	record.UpdatedAt = time.Now().Unix()
}

func ownProtocolSender(current *state.NativeState, sender string) bool {
	jid, err := types.ParseJID(sender)
	if err != nil || jid.IsEmpty() {
		return false
	}
	pn, identity := ownMessageJIDs(current)
	return jid.ToNonAD().String() == pn || jid.ToNonAD().String() == identity
}

func retryableDecryptionFailure(err error) bool {
	failure := shared.ToProtoError(err)
	return failure.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED && failure.GetRetryable()
}

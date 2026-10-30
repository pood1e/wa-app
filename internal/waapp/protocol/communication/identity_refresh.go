package communication

import (
	"bytes"
	"context"
	"errors"
	"time"

	"go.mau.fi/libsignal/keys/prekey"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) refreshSenderIdentity(ctx context.Context, input wacore.EngineDecryptInput) (bool, error) {
	current, err := c.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return false, err
	}
	payload, ok := current.MessagePayloads[input.PayloadRef]
	if !ok || NormalizeNativeSignalEncType(payload.EncType) != "pkmsg" || current.Signal.RemoteIdentities[SignalSenderKey(payload.Sender)] == "" {
		return false, nil
	}
	raw, err := wire.DecodeB64Any(payload.Payload)
	if err != nil {
		return false, err
	}
	message, err := ParsePreKeySignalMessage(raw)
	if err != nil || validateRemoteSignalIdentity(&current, payload.Sender, message.IdentityKey) == nil {
		return false, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, update, err := c.sendIQLocked(queryCtx, current, c.Input.RegisteredIdentityID, c.Input.AppVersion, peerPreKeyRequest(payload.Sender), "peer identity refresh timed out")
	if err != nil {
		return false, err
	}
	bundle, err := peerPreKeyFromResponse(response, payload.Sender)
	if err != nil {
		return false, err
	}
	ApplyChatdSessionUpdateState(&current, update)
	if err := c.Engine.decryptWithVerifiedIdentity(ctx, input, &current, payload, bundle, message.IdentityKey); err != nil {
		return false, err
	}
	return true, nil
}

func (e *messagingService) decryptWithVerifiedIdentity(ctx context.Context, input wacore.EngineDecryptInput, current *state.NativeState, payload state.NativeMessagePayload, bundle *prekey.Bundle, messageIdentity []byte) error {
	actual, err := wire.WithSignalCurvePrefix(messageIdentity)
	if err != nil || !bytes.Equal(actual, bundle.IdentityKey().PublicKey().Serialize()) {
		return errors.New("message identity does not match the verified peer bundle")
	}
	// Commit the new peer identity only after its prekey message authenticates.
	delete(current.Signal.RemoteIdentities, SignalSenderKey(payload.Sender))
	delete(current.Signal.Sessions, SignalSenderKey(payload.Sender))
	_, err = e.decryptWithState(ctx, input, current, payload)
	return err
}

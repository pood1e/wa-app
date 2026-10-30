package communication

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/libsignal/keys/identity"
	"go.mau.fi/libsignal/keys/prekey"
	"go.mau.fi/libsignal/ratchet"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ensureDirectSignalSession(ctx context.Context, connection *ChatdSession, current *state.NativeState, input wacore.EngineMessageInput, target string, timeout time.Duration) ([]ChatdReceivedItem, ChatdSessionUpdate, error) {
	if _, _, exists := ExactSignalSession(current.Signal.Sessions, target); exists {
		return nil, connection.Update(), nil
	}
	request := peerPreKeyRequest(target)
	response, items, update, err := connection.SendIQ(ctx, input, request, timeout, "peer prekey request timed out")
	if err != nil {
		return items, update, err
	}
	bundle, err := peerPreKeyFromResponse(response, target)
	if err == nil {
		err = initializeOutboundSession(current, target, bundle)
	}
	return items, update, err
}

func peerPreKeyRequest(target string) wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": NewTextProviderMessageID(""), "type": "get", "xmlns": "encrypt", "to": "s.whatsapp.net"},
		Content: []wire.ChatdNode{{Tag: "key", Content: []wire.ChatdNode{{Tag: "user", Attrs: map[string]string{"jid": target, "reason": "identity"}}}}}}
}

func peerPreKeyFromResponse(response wire.ChatdNode, target string) (*prekey.Bundle, error) {
	if err := ChatdIQError(response); err != nil {
		return nil, err
	}
	list, ok := ChatdChild(response, "list")
	if !ok {
		return nil, errors.New("peer prekey response is missing its list")
	}
	for _, user := range wire.ChatdChildren(list) {
		if user.Tag != "user" || wacore.NormalizeWAJID(user.Attrs["jid"]) != target {
			continue
		}
		return preKeyBundleFromNode(user)
	}
	return nil, errors.New("peer prekey response does not contain the requested identity")
}

func initializeOutboundSession(current *state.NativeState, target string, bundle *prekey.Bundle) error {
	ensureSignalLogger()
	remoteIdentity := bundle.IdentityKey().PublicKey().Serialize()
	if err := validateRemoteSignalIdentity(current, target, remoteIdentity); err != nil {
		return err
	}
	localIdentity, err := signalKeyPair(current.Signal.Identity)
	if err != nil {
		return err
	}
	base, err := ecc.GenerateKeyPair()
	if err != nil {
		return err
	}
	sendingRatchet, err := ecc.GenerateKeyPair()
	if err != nil {
		return err
	}
	parameters := ratchet.NewSenderParameters(identity.NewKeyPair(identity.NewKey(localIdentity.PublicKey()), localIdentity.PrivateKey()), base,
		bundle.IdentityKey(), bundle.SignedPreKey(), bundle.SignedPreKey(), bundle.PreKey())
	initial, err := ratchet.CalculateSenderSession(parameters)
	if err != nil {
		return err
	}
	sending, err := initial.RootKey.CreateChain(bundle.SignedPreKey(), sendingRatchet)
	if err != nil {
		return err
	}
	private := sendingRatchet.PrivateKey().Serialize()
	registration := int(bundle.RegistrationID())
	previous := 0
	pending := &protocolv1.SignalPendingPreKey{BaseKey: base.PublicKey().Serialize(), SignedPreKeyId: bundle.SignedPreKeyID()}
	if !bundle.PreKeyID().IsEmpty {
		id := bundle.PreKeyID().Value
		pending.PreKeyId = &id
	}
	current.Signal.Sessions[SignalSenderKey(target)] = state.NativeSignalSession{
		Sender: target, Version: 3, RemoteIdentityPublic: wire.B64u(remoteIdentity), RootKey: wire.B64u(sending.RootKey.Bytes()),
		SenderRatchetPublic: wire.B64u(sendingRatchet.PublicKey().Serialize()), SenderRatchetPrivate: wire.B64u(private[:]),
		SenderChain:    &state.NativeSenderChain{RatchetKey: wire.B64u(sendingRatchet.PublicKey().Serialize()), ChainKey: wire.B64u(sending.ChainKey.Key())},
		ReceiverChains: map[string]state.NativeReceiverChain{}, PreviousCounter: &previous, RemoteRegistrationID: &registration,
		AliceBaseKey: wire.B64u(base.PublicKey().Serialize()), PendingPreKey: pending,
	}
	current.Signal.RemoteIdentities[SignalSenderKey(target)] = wire.B64u(remoteIdentity)
	return nil
}

func signalKeyPair(pair wire.NativeCurveKeyPair) (*ecc.ECKeyPair, error) {
	public, err := pair.PublicBytes()
	if err != nil {
		return nil, err
	}
	private, err := pair.PrivateBytes()
	if err != nil {
		return nil, err
	}
	return ecc.NewECKeyPair(ecc.NewDjbECPublicKey([32]byte(public)), ecc.NewDjbECPrivateKey([32]byte(private))), nil
}

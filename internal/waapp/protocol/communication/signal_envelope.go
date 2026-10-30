package communication

import (
	"bytes"
	"crypto/rand"
	"errors"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/libsignal/keys/identity"
	"go.mau.fi/libsignal/protocol"
	"go.mau.fi/libsignal/serialize"
	"go.mau.fi/libsignal/util/optional"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func encryptDirectSignalPayload(current *state.NativeState, target string, plaintext []byte) ([]byte, string, error) {
	ensureSignalLogger()
	key, session, exists := ExactSignalSession(current.Signal.Sessions, target)
	if !exists {
		return nil, "", errors.New("peer Signal session is missing")
	}
	if err := EnsureOutboundSignalChain(&session); err != nil {
		return nil, "", err
	}
	raw, err := EncryptSignalPlaintext(current, &session, padSignalMessage(plaintext))
	if err != nil {
		return nil, "", err
	}
	kind := "msg"
	if pending := session.PendingPreKey; pending != nil {
		serializer := serialize.NewProtoBufSerializer()
		message, err := protocol.NewSignalMessageFromBytes(raw, serializer.SignalMessage)
		if err != nil {
			return nil, "", err
		}
		base, err := ecc.DecodePoint(pending.GetBaseKey(), 0)
		if err != nil {
			return nil, "", err
		}
		local, err := signalKeyPair(current.Signal.Identity)
		if err != nil {
			return nil, "", err
		}
		id := optional.NewEmptyUint32()
		if pending.PreKeyId != nil {
			id = optional.NewOptionalUint32(pending.GetPreKeyId())
		}
		envelope, err := protocol.NewPreKeySignalMessage(session.Version, uint32(current.Signal.RegistrationID), id,
			pending.GetSignedPreKeyId(), base, identity.NewKey(local.PublicKey()), message, serializer.PreKeySignalMessage, serializer.SignalMessage)
		if err != nil {
			return nil, "", err
		}
		raw, kind = envelope.Serialize(), "pkmsg"
	}
	current.Signal.Sessions[key] = session
	return raw, kind, nil
}

func padSignalMessage(plaintext []byte) []byte {
	var random [1]byte
	_, _ = rand.Read(random[:])
	padding := random[0]%16 + 1
	return append(bytes.Clone(plaintext), bytes.Repeat([]byte{padding}, int(padding))...)
}

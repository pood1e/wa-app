package communication

import (
	"fmt"
	"slices"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func decapsulatePreKeyMessage(current *state.NativeState, message PreKeySignalMessage) ([]byte, error) {
	id := *message.KyberPreKeyID
	for _, key := range current.Signal.PQOneTimePreKeys {
		if int(key.GetId()) == id {
			return wire.DecapsulateSignalPQ(key.GetPrivate(), message.KyberCiphertext)
		}
	}
	if key := current.Signal.PQLastResortPreKey; key != nil && int(key.GetId()) == id {
		return wire.DecapsulateSignalPQ(key.GetPrivate(), message.KyberCiphertext)
	}
	return nil, fmt.Errorf("PQ prekey %d not found", id)
}

func consumePreKeys(current *state.NativeState, message PreKeySignalMessage) {
	if message.PreKeyID != nil {
		current.Signal.OneTimePreKeys = slices.DeleteFunc(current.Signal.OneTimePreKeys, func(key state.NativeSignalPreKey) bool {
			return int(key.ID) == *message.PreKeyID
		})
	}
	if message.KyberPreKeyID == nil {
		return
	}
	current.Signal.PQOneTimePreKeys = slices.DeleteFunc(current.Signal.PQOneTimePreKeys, func(key *protocolv1.SignalPQPreKey) bool {
		return int(key.GetId()) == *message.KyberPreKeyID
	})
}

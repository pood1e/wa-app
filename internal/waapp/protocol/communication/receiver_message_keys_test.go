package communication

import (
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestSkippedMessageKeysStayBoundedWithoutMutatingSavedChains(t *testing.T) {
	chain := state.NativeReceiverChain{ChainKey: wire.B64u(make([]byte, 32))}
	if _, _, err := receiverMessageKeys(chain, maxSkippedMessageKeys+1); err == nil {
		t.Fatal("unbounded counter gap was accepted")
	}
	_, first, err := receiverMessageKeys(chain, maxSkippedMessageKeys)
	if err != nil || len(first.SkippedMessageKeys) != maxSkippedMessageKeys || len(chain.SkippedMessageKeys) != 0 {
		t.Fatal("skipped keys were lost or the saved chain was mutated")
	}
	_, next, err := receiverMessageKeys(first, 2*maxSkippedMessageKeys+1)
	if err != nil || len(next.SkippedMessageKeys) != maxSkippedMessageKeys {
		t.Fatal("skipped message key retention exceeded its bound")
	}
	if first.SkippedMessageKeys[0] == nil || next.SkippedMessageKeys[0] != nil {
		t.Fatal("eviction changed the saved chain or retained the oldest keys")
	}
}

package communication

import (
	"fmt"
	"maps"
	"math"
	"sort"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

const maxSkippedMessageKeys = 2000

// receiverMessageKeys stages chain advancement; callers commit only after authentication.
func receiverMessageKeys(chain state.NativeReceiverChain, counter int) (*protocolv1.SignalMessageKeys, state.NativeReceiverChain, error) {
	if counter < 0 || uint64(counter) > math.MaxUint32 {
		return nil, chain, fmt.Errorf("invalid Signal message counter")
	}
	chain.SkippedMessageKeys = maps.Clone(chain.SkippedMessageKeys)
	if keys := chain.SkippedMessageKeys[uint32(counter)]; keys != nil {
		delete(chain.SkippedMessageKeys, uint32(counter))
		return keys, chain, nil
	}
	if counter < chain.Index {
		return nil, chain, fmt.Errorf("Signal message key is no longer available")
	}
	if counter-chain.Index > maxSkippedMessageKeys {
		return nil, chain, fmt.Errorf("message counter exceeds the supported forward gap")
	}
	chainKey, err := wire.DecodeB64Any(chain.ChainKey)
	if err != nil {
		return nil, chain, err
	}
	if chain.SkippedMessageKeys == nil {
		chain.SkippedMessageKeys = make(map[uint32]*protocolv1.SignalMessageKeys)
	}
	for chain.Index < counter {
		chain.SkippedMessageKeys[uint32(chain.Index)] = DeriveMessageKeys(chainKey)
		chainKey = NextChainKey(chainKey)
		chain.Index++
	}
	keys := DeriveMessageKeys(chainKey)
	chain.ChainKey, chain.Index = wire.B64u(NextChainKey(chainKey)), counter+1
	if excess := len(chain.SkippedMessageKeys) - maxSkippedMessageKeys; excess > 0 {
		indices := make([]uint32, 0, len(chain.SkippedMessageKeys))
		for index := range chain.SkippedMessageKeys {
			indices = append(indices, index)
		}
		sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
		for _, index := range indices[:excess] {
			delete(chain.SkippedMessageKeys, index)
		}
	}
	return keys, chain, nil
}

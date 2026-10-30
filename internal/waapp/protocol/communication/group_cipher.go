package communication

import (
	"context"
	"errors"

	"go.mau.fi/libsignal/groups"
	"go.mau.fi/libsignal/groups/state/record"
	"go.mau.fi/libsignal/protocol"
	"go.mau.fi/libsignal/serialize"
	"go.mau.fi/whatsmeow/types"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

type senderKeyStore struct {
	current    *state.NativeState
	commit     bool
	serializer *serialize.Serializer
}

func (s senderKeyStore) LoadSenderKey(ctx context.Context, name *protocol.SenderKeyName) (*record.SenderKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw := s.current.SenderKeyRecords[senderKeyStorageKey(name)]
	if len(raw) == 0 {
		return record.NewSenderKey(s.serializer.SenderKeyRecord, s.serializer.SenderKeyState), nil
	}
	return record.NewSenderKeyFromBytes(raw, s.serializer.SenderKeyRecord, s.serializer.SenderKeyState)
}

func (s senderKeyStore) StoreSenderKey(ctx context.Context, name *protocol.SenderKeyName, value *record.SenderKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.commit {
		return nil
	}
	if s.current.SenderKeyRecords == nil {
		s.current.SenderKeyRecords = map[string][]byte{}
	}
	s.current.SenderKeyRecords[senderKeyStorageKey(name)] = value.Serialize()
	return nil
}

func senderKeyStorageKey(name *protocol.SenderKeyName) string {
	return name.GroupID() + "|" + name.Sender().String()
}

func groupSenderKeyName(group, sender string) (*protocol.SenderKeyName, error) {
	groupJID, err := types.ParseJID(group)
	if err != nil || groupJID.Server != types.GroupServer {
		return nil, errors.New("group address is invalid")
	}
	jid, err := types.ParseJID(sender)
	if err != nil || jid.User == "" || jid.Server == types.GroupServer {
		return nil, errors.New("group sender address is invalid")
	}
	return protocol.NewSenderKeyName(groupJID.String(), jid.SignalAddress()), nil
}

func newGroupCrypto(current *state.NativeState, name *protocol.SenderKeyName, commit bool) (*groups.SessionBuilder, *groups.GroupCipher) {
	ensureSignalLogger()
	serializer := serialize.NewProtoBufSerializer()
	store := senderKeyStore{current: current, commit: commit, serializer: serializer}
	builder := groups.NewGroupSessionBuilder(store, serializer)
	return builder, groups.NewGroupCipher(builder, name, store)
}

func decryptGroupMessage(current *state.NativeState, group, sender string, raw []byte, commit bool) (SignalDecryptOutput, error) {
	ensureSignalLogger()
	if len(raw) <= 65 {
		return SignalDecryptOutput{}, errors.New("sender-key message is truncated")
	}
	name, err := groupSenderKeyName(group, sender)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	serializer := serialize.NewProtoBufSerializer()
	message, err := protocol.NewSenderKeyMessageFromBytes(raw, serializer.SenderKeyMessage)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	_, cipher := newGroupCrypto(current, name, commit)
	plain, err := cipher.Decrypt(context.Background(), message)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	return SignalDecryptOutput{EncType: "skmsg", Variant: "sender_key", Version: 3, Counter: int(message.Iteration()), Plaintext: plain}, nil
}

func processGroupDistribution(ctx context.Context, current *state.NativeState, group, sender string, raw []byte) error {
	ensureSignalLogger()
	if len(raw) < 2 {
		return errors.New("sender-key distribution is truncated")
	}
	name, err := groupSenderKeyName(group, sender)
	if err != nil {
		return err
	}
	serializer := serialize.NewProtoBufSerializer()
	message, err := protocol.NewSenderKeyDistributionMessageFromBytes(raw, serializer.SenderKeyDistributionMessage)
	if err != nil {
		return err
	}
	builder, _ := newGroupCrypto(current, name, true)
	return builder.Process(ctx, name, message)
}

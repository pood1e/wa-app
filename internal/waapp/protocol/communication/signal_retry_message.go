package communication

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"time"

	"go.mau.fi/libsignal/keys/prekey"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func (p *messagePreparation) retryMessage(ctx context.Context, receipt wire.ChatdNode, request signalRetryRequest) (wire.ChatdNode, error) {
	stored := findSentMessage(p.current, request.chat.String(), request.id)
	if stored == nil || time.Since(time.Unix(stored.GetTimestamp(), 0)) > sentMessageLifetime || !sentToDevice(p.current, stored, request.sender.String()) {
		return wire.ChatdNode{}, errSignalRetryRejected
	}
	key := protocolRetryKey(p.current, request.chat.String(), request.sender.String(), request.id)
	retryState := p.current.SignalRetries[key]
	if retryState.GetRespondedCount() >= request.count {
		return wire.ChatdNode{}, nil
	}
	message := new(waE2E.Message)
	if err := proto.Unmarshal(stored.GetMessage(), message); err != nil {
		return wire.ChatdNode{}, err
	}
	if request.chat.Server == "g.us" {
		if err := p.authorizeGroupRetry(ctx, request); err != nil {
			return wire.ChatdNode{}, err
		}
		_, own := ownMessageJIDs(p.current)
		name, err := groupSenderKeyName(request.chat.String(), own)
		if err != nil {
			return wire.ChatdNode{}, err
		}
		builder, _ := newGroupCrypto(p.current, name, true)
		distribution, err := builder.Create(ctx, name)
		if err != nil {
			return wire.ChatdNode{}, err
		}
		message.SenderKeyDistributionMessage = &waE2E.SenderKeyDistributionMessage{GroupID: proto.String(request.chat.String()), AxolotlSenderKeyDistributionMessage: distribution.Serialize()}
	}
	identity := signalIdentityForDevice(p.current, request.sender.String())
	if err := p.retrySession(ctx, receipt, request, identity); err != nil {
		return wire.ChatdNode{}, err
	}
	plaintext, err := proto.Marshal(message)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	ciphertext, kind, err := encryptDirectSignalPayload(p.current, identity, plaintext)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	node := wire.ChatdNode{Tag: "message", Attrs: map[string]string{"to": receipt.Attrs["from"], "id": request.id, "t": strconv.FormatInt(stored.GetTimestamp(), 10)}, Content: []wire.ChatdNode{
		{Tag: "enc", Attrs: map[string]string{"type": kind, "v": "2", "count": strconv.FormatUint(uint64(request.count), 10)}, Content: ciphertext},
	}}
	for _, attribute := range []string{"participant", "recipient"} {
		if value := receipt.Attrs[attribute]; value != "" {
			node.Attrs[attribute] = value
		}
	}
	if request.chat.Server != "g.us" {
		node.Attrs["device_fanout"] = "false"
	}
	applyMessageNodeAttributes(&node, message)
	if stored.GetEdit() != "" {
		node.Attrs["edit"] = stored.GetEdit()
	}
	p.current.SignalRetries[key] = &protocolv1.SignalRetryState{RespondedCount: request.count, UpdatedAt: time.Now().Unix()}
	return node, nil
}

func (p *messagePreparation) authorizeGroupRetry(ctx context.Context, request signalRetryRequest) error {
	query, err := groupInfoIQ(request.chat.String(), NewTextProviderMessageID(""))
	if err != nil {
		return err
	}
	response, err := p.query(ctx, query, "retry group membership query timed out")
	if err != nil {
		return err
	}
	group, err := parseGroupResponse(response)
	if err != nil {
		return err
	}
	p.record(nil, ChatdSessionUpdate{ContactHints: groupContactHints(group)})
	identity := signalIdentityForDevice(p.current, request.sender.ToNonAD().String())
	for _, member := range group.GetMembers() {
		if signalIdentityForDevice(p.current, member.GetMemberRef()) == identity || signalIdentityForDevice(p.current, member.GetPhoneRef()) == identity {
			return nil
		}
	}
	return errSignalRetryRejected
}

func (p *messagePreparation) retrySession(ctx context.Context, node wire.ChatdNode, request signalRetryRequest, identity string) error {
	registration, err := preKeyNodeBytes(node, "registration", 4)
	if err != nil {
		return errSignalRetryRejected
	}
	var bundle *prekey.Bundle
	if _, hasKeys := ChatdChild(node, "keys"); hasKeys {
		bundle, err = preKeyBundleFromNode(node)
		if err != nil {
			return errSignalRetryRejected
		}
	} else {
		_, session, exists := ExactSignalSession(p.current.Signal.Sessions, identity)
		if exists && request.count < 2 && session.RemoteRegistrationID != nil && uint32(*session.RemoteRegistrationID) == binary.BigEndian.Uint32(registration) {
			return nil
		}
	}
	if bundle == nil || validateRemoteSignalIdentity(p.current, identity, bundle.IdentityKey().PublicKey().Serialize()) != nil {
		response, queryErr := p.query(ctx, peerPreKeyRequest(request.sender.String()), "retry peer prekey query timed out")
		if queryErr != nil {
			return queryErr
		}
		verified, queryErr := peerPreKeyFromResponse(response, request.sender.String())
		if queryErr != nil {
			return queryErr
		}
		if bundle != nil && !bytes.Equal(bundle.IdentityKey().PublicKey().Serialize(), verified.IdentityKey().PublicKey().Serialize()) {
			return errSignalRetryRejected
		}
		bundle = verified
		if err := validateRemoteSignalIdentity(p.current, identity, bundle.IdentityKey().PublicKey().Serialize()); err != nil {
			delete(p.current.Signal.RemoteIdentities, SignalSenderKey(identity))
			delete(p.current.Signal.Sessions, SignalSenderKey(identity))
		}
	}
	return initializeOutboundSession(p.current, identity, bundle)
}

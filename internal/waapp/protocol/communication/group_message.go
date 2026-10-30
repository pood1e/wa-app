package communication

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func (p *messagePreparation) groupMessage(ctx context.Context, target, id string, content *waE2E.Message) (wire.ChatdNode, error) {
	query, err := groupInfoIQ(target, NewTextProviderMessageID(""))
	if err != nil {
		return wire.ChatdNode{}, err
	}
	response, err := p.query(ctx, query, "group metadata query timed out")
	if err != nil {
		return wire.ChatdNode{}, err
	}
	group, err := parseGroupResponse(response)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	p.record(nil, ChatdSessionUpdate{ContactHints: groupContactHints(group)})
	members := make([]string, 0, len(group.GetMembers()))
	for _, member := range group.GetMembers() {
		members = append(members, member.GetMemberRef())
	}
	devices, err := p.recipientDevices(ctx, members)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	hash := participantDeviceHash(devices)
	ownPN, ownIdentity := ownMessageJIDs(p.current)
	devices = slices.DeleteFunc(devices, func(ref string) bool { return ref == ownPN || ref == ownIdentity })
	if len(devices) == 0 {
		return wire.ChatdNode{}, errors.New("group has no other recipient devices")
	}
	name, err := groupSenderKeyName(target, ownIdentity)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	builder, cipher := newGroupCrypto(p.current, name, true)
	distribution, err := builder.Create(ctx, name)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	plaintext, err := proto.Marshal(&waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{
		GroupID: proto.String(target), AxolotlSenderKeyDistributionMessage: distribution.Serialize(),
	}})
	if err != nil {
		return wire.ChatdNode{}, err
	}
	participants, err := p.encryptForDevices(ctx, devices, plaintext)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	body, err := proto.Marshal(content)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	message, err := cipher.Encrypt(ctx, padSignalMessage(body))
	if err != nil {
		return wire.ChatdNode{}, err
	}
	attrs := map[string]string{"id": id, "to": target, "type": "text", "phash": hash}
	if strings.HasSuffix(ownIdentity, "@lid") {
		attrs["addressing_mode"] = "lid"
	}
	return wire.ChatdNode{Tag: "message", Attrs: attrs, Content: []wire.ChatdNode{
		{Tag: "participants", Content: participants},
		{Tag: "enc", Attrs: map[string]string{"type": "skmsg", "v": "2"}, Content: message.SignedSerialize()},
	}}, nil
}

func participantDeviceHash(devices []string) string {
	values := make([]string, 0, len(devices))
	for _, device := range devices {
		jid, err := types.ParseJID(device)
		if err == nil {
			values = append(values, jid.ADString())
		}
	}
	slices.Sort(values)
	hash := sha256.Sum256([]byte(strings.Join(values, "")))
	return "2:" + base64.RawStdEncoding.EncodeToString(hash[:6])
}

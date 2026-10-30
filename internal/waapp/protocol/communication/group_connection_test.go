package communication

import (
	"context"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const testGroupRef = "120363000000000000@g.us"

func groupTestConnection(t *testing.T, current state.NativeState) (*Connection, *ChatdTransport, *historicalMessageState) {
	t.Helper()
	engine, repository := newHistoricalMessageEngine(t, current, newMessageParty(t))
	connection := NewConnection(engine, ConnectionOptions{Context: t.Context(), Input: wacore.EngineMessageInput{ClientProfileID: "current"}})
	t.Cleanup(func() { _ = connection.Close() })
	session, peer, server := syntheticChatdPipe(t)
	if err := server.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection.installSession(session)
	return connection, peer, repository
}

func groupTestIQ(t *testing.T, peer *ChatdTransport, children ...wire.ChatdNode) wire.ChatdNode {
	t.Helper()
	request, err := peer.ReadNode()
	if err != nil {
		t.Fatal(err)
	}
	if request.Tag != "iq" || request.Attrs["xmlns"] != "w:g2" {
		t.Fatal("group operation did not use native w:g2 routing")
	}
	if err := peer.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": request.Attrs["id"], "type": "result"}, Content: children}); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestGroupMutationCompletesOnACKUsingOwnedConnection(t *testing.T) {
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		t.Run(platform.String(), func(t *testing.T) {
			current := newMessageParty(t)
			current.Client.Platform = platform
			connection, peer, _ := groupTestConnection(t, current)
			session := connection.Session
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- connection.UpdateGroup(ctx, &waappv1.UpdateGroupRequest{GroupRef: testGroupRef, Change: &waappv1.UpdateGroupRequest_Name{Name: "New 中文"}})
			}()
			request := groupTestIQ(t, peer)
			subject, _ := ChatdChild(request, "subject")
			if request.Attrs["to"] != testGroupRef || request.Attrs["type"] != "set" || ChatdNodeText(subject) != "New 中文" {
				t.Fatal("native subject update changed its destination or UTF-8 body")
			}
			if err := <-done; err != nil {
				t.Fatal("acknowledged mutation waited for an extra metadata read", err)
			}
			if connection.Session != session || connection.ownerCtx.Err() != nil {
				t.Fatal("group mutation replaced or stopped the account connection")
			}
		})
	}
}

func TestDescriptionReadsRevisionBeforeMutation(t *testing.T) {
	connection, peer, _ := groupTestConnection(t, newMessageParty(t))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- connection.UpdateGroup(ctx, &waappv1.UpdateGroupRequest{GroupRef: testGroupRef, Change: &waappv1.UpdateGroupRequest_Description{Description: "新描述"}})
	}()
	read := groupTestIQ(t, peer, wire.ChatdNode{Tag: "group", Attrs: map[string]string{"id": testGroupRef}, Content: []wire.ChatdNode{{Tag: "description", Attrs: map[string]string{"id": "previous-revision"}}}})
	write := groupTestIQ(t, peer)
	description, _ := ChatdChild(write, "description")
	body, _ := ChatdChild(description, "body")
	if read.Attrs["type"] != "get" || write.Attrs["type"] != "set" || description.Attrs["prev"] != "previous-revision" || description.Attrs["id"] == "" || ChatdNodeText(body) != "新描述" {
		t.Fatal("description update lost native compare-and-set semantics")
	}
	if err := <-done; err != nil {
		t.Fatal("description update failed after acknowledgement", err)
	}
	deleted, err := groupChangeNode(&waappv1.UpdateGroupRequest{Change: &waappv1.UpdateGroupRequest_Description{}}, "previous-revision")
	if err != nil || deleted.Attrs["delete"] != "true" || deleted.Attrs["prev"] != "previous-revision" || deleted.Attrs["id"] != "" || len(wire.ChatdChildren(deleted)) != 0 {
		t.Fatal("empty description did not use the native deletion form")
	}
}

func TestGroupMetadataLearnsOwnLIDAndPersistsIt(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	connection, peer, _ := groupTestConnection(t, current)
	type result struct {
		group *waappv1.GroupChat
		err   error
	}
	done := make(chan result, 1)
	go func() { group, err := connection.GetGroup(t.Context(), testGroupRef); done <- result{group, err} }()
	groupTestIQ(t, peer, wire.ChatdNode{Tag: "group", Attrs: map[string]string{"id": testGroupRef}, Content: []wire.ChatdNode{{Tag: "participant", Attrs: map[string]string{"jid": "100001@lid", "phone_number": current.RegistrationJID, "type": "superadmin"}}}})
	got := <-done
	if got.err != nil || got.group.GetSelfMemberRef() != "100001@lid" {
		t.Fatal("native group metadata did not identify the current member", got.err)
	}
	restored, err := connection.loadState(t.Context(), "current")
	if err != nil || signalIdentityForDevice(&restored, current.RegistrationJID) != "100001@lid" {
		t.Fatal("group identity mapping was not checkpointed", err)
	}
}

func TestGroupParticipantInputDeduplicatesCanonicalIdentities(t *testing.T) {
	current := newMessageParty(t)
	current.ContactHints = []wacore.WAContactHint{{PNJID: "12025550123@s.whatsapp.net", LIDJID: "100001@lid"}}
	connection, _, _ := groupTestConnection(t, current)
	session := groupSession{connection: connection, current: current}
	nodes, err := session.participantNodes([]string{"12025550123:2@s.whatsapp.net", "100001@lid", "100001:4@lid"}, "create")
	if err != nil || len(nodes) != 1 || nodes[0].Attrs["jid"] != "100001@lid" || nodes[0].Attrs["phone_number"] != "12025550123@s.whatsapp.net" {
		t.Fatal("PN/LID/device aliases produced duplicate group actions", err)
	}
	if _, err := session.participantNodes([]string{testGroupRef}, "add"); err == nil {
		t.Fatal("a group was accepted as a participant")
	}
}

func TestGroupApprovalRetainsEarlierResultsWhenLaterRequestFails(t *testing.T) {
	connection, peer, _ := groupTestConnection(t, newMessageParty(t))
	type result struct {
		members []*waappv1.GroupParticipantResult
		err     error
	}
	done := make(chan result, 1)
	go func() {
		members, err := connection.UpdateGroupParticipants(t.Context(), &waappv1.UpdateGroupParticipantsRequest{GroupRef: testGroupRef, Action: waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_APPROVE_JOIN, MemberRefs: []string{"100001@lid", "100002@lid"}})
		done <- result{members, err}
	}()
	first := groupTestIQ(t, peer, wire.ChatdNode{Tag: "membership_requests_action", Content: []wire.ChatdNode{{Tag: "approve", Content: []wire.ChatdNode{{Tag: "participant", Attrs: map[string]string{"jid": "100001@lid"}}}}}})
	action, _ := ChatdChild(first, "membership_requests_action")
	approve, _ := ChatdChild(action, "approve")
	if len(wire.ChatdChildren(approve)) != 1 {
		t.Fatal("approval did not follow the native single-candidate request")
	}
	second, err := peer.ReadNode()
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": second.Attrs["id"], "type": "error"}, Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "500"}}}}); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err == nil || len(got.members) != 1 || got.members[0].GetMemberRef() != "100001@lid" || got.members[0].GetError() != nil {
		t.Fatal("later approval failure discarded an earlier confirmed result")
	}
}

func TestGroupCreationRetainsRemoteAcknowledgementAfterCheckpointFailure(t *testing.T) {
	connection, peer, repository := groupTestConnection(t, newMessageParty(t))
	repository.fail = "current"
	type result struct {
		response *waappv1.GroupResponse
		err      error
	}
	done := make(chan result, 1)
	go func() {
		response, err := connection.CreateGroup(t.Context(), &waappv1.CreateGroupRequest{Name: "Fixture", MemberRefs: []string{"100001@lid"}})
		done <- result{response, err}
	}()
	groupTestIQ(t, peer, wire.ChatdNode{Tag: "group", Attrs: map[string]string{"id": testGroupRef, "subject": "Fixture"}, Content: []wire.ChatdNode{{Tag: "participant", Attrs: map[string]string{"jid": "100001@lid", "phone_number": "12025550123@s.whatsapp.net"}}}})
	got := <-done
	if got.err == nil || got.response.GetGroup().GetGroupRef() != testGroupRef || len(got.response.GetParticipantResults()) != 1 || got.response.GetParticipantResults()[0].GetError() != nil {
		t.Fatal("checkpoint failure hid remote creation and invited duplicate creation")
	}
}

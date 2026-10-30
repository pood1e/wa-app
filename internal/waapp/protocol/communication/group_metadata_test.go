package communication

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestGroupMetadataPreservesRolesAndNativeSettings(t *testing.T) {
	response := wire.ChatdNode{Content: []wire.ChatdNode{{Tag: "group", Attrs: map[string]string{"id": "120363000000000000", "subject": "中文群聊", "size": "4"}, Content: []wire.ChatdNode{
		{Tag: "participant", Attrs: map[string]string{"jid": "100001@lid", "phone_number": "12025550123@s.whatsapp.net", "type": "superadmin"}},
		{Tag: "participant", Attrs: map[string]string{"jid": "100002@lid", "type": "admin"}},
		{Tag: "participant", Attrs: map[string]string{"jid": "100003@lid"}},
		{Tag: "participant", Attrs: map[string]string{"jid": "100004@lid", "error": "403"}},
		{Tag: "description", Attrs: map[string]string{"id": "old-description"}, Content: []wire.ChatdNode{{Tag: "body", Content: []byte("群描述")}}},
		{Tag: "locked"}, {Tag: "announcement"}, {Tag: "member_add_mode", Content: []byte("all_member_add")},
		{Tag: "membership_approval_mode", Content: []wire.ChatdNode{{Tag: "group_join", Attrs: map[string]string{"state": "on"}}}},
	}}}}
	group, err := parseGroupResponse(response)
	if err != nil || group.GetGroupRef() != "120363000000000000@g.us" || group.GetName() != "中文群聊" || len(group.GetMembers()) != 3 || group.GetMemberCount() != 4 {
		t.Fatalf("group metadata lost identity, members or reported size: %v", err)
	}
	if group.GetDescription() != "群描述" || group.GetDescriptionId() != "old-description" || !group.GetLocked() || !group.GetAnnouncementOnly() || !group.GetAllMembersCanAdd() || !group.GetJoinApproval() {
		t.Fatal("native group settings or description revision were lost")
	}
	for i, role := range []waappv1.GroupMemberRole{waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_OWNER, waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_ADMIN, waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_MEMBER} {
		if group.GetMembers()[i].GetRole() != role {
			t.Fatalf("member %d role = %s, want %s", i, group.GetMembers()[i].GetRole(), role)
		}
	}
}

func TestGroupParticipantResultsRetainPartialRejectionsAndAliases(t *testing.T) {
	node := wire.ChatdNode{Content: []wire.ChatdNode{
		{Tag: "participant", Attrs: map[string]string{"jid": "100001@lid", "phone_number": "12025550123@s.whatsapp.net"}},
		{Tag: "participant", Attrs: map[string]string{"jid": "100002@lid"}, Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "403"}}}},
	}}
	results := groupParticipantResults(node, []string{"12025550123@s.whatsapp.net", "100002@lid", "100003@lid"})
	if len(results) != 3 || results[0].GetMemberRef() != "100001@lid" || results[0].GetError() != nil || results[1].GetError() == nil || results[2].GetError() == nil {
		t.Fatal("partial membership failure or missing acknowledgement was reported as success")
	}
}

func TestGroupJoinRequestsUseNativeEntryAndTimestamp(t *testing.T) {
	response := wire.ChatdNode{Content: []wire.ChatdNode{{Tag: "membership_approval_requests", Content: []wire.ChatdNode{{Tag: "membership_approval_request", Attrs: map[string]string{"jid": "100001@lid", "phone_number": "12025550123@s.whatsapp.net", "request_time": "1700000000"}}}}}}
	requests, err := parseGroupJoinRequests(response)
	if err != nil || len(requests) != 1 || requests[0].GetMember().GetRole() != waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_UNSPECIFIED || requests[0].GetRequestedAt().GetSeconds() != 1700000000 {
		t.Fatalf("native join request was not retained: %v", err)
	}
	if _, err := parseGroupJoinRequests(wire.ChatdNode{Tag: "iq"}); err == nil {
		t.Fatal("missing approval list was accepted as an empty list")
	}
}

func TestGroupInviteValidationDoesNotAcceptForeignURLs(t *testing.T) {
	const code = "SyntheticInviteCode0123"
	for _, value := range []string{code, " https://chat.whatsapp.com/" + code + "?mode=ac_t "} {
		if got, err := groupInviteCode(value); err != nil || got != code {
			t.Fatalf("valid invite was rejected: %v", err)
		}
	}
	for _, value := range []string{"", "http://chat.whatsapp.com/" + code, "https://chat.whatsapp.com.evil.test/" + code, "https://user@chat.whatsapp.com/" + code, "https://chat.whatsapp.com/a/b", "javascript:fixture"} {
		if _, err := groupInviteCode(value); err == nil {
			t.Fatal("invalid or foreign invite URL was accepted")
		}
	}
}

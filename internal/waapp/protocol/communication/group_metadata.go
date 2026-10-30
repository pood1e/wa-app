package communication

import (
	"errors"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow/types"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func normalizeGroupRef(ref string) (string, error) {
	jid, err := types.ParseJID(strings.TrimSpace(ref))
	if err != nil || jid.Server != types.GroupServer || jid.User == "" || jid.Device != 0 {
		return "", errors.New("group address is invalid")
	}
	return jid.String(), nil
}

func parseGroupResponse(response wire.ChatdNode) (*waappv1.GroupChat, error) {
	node, ok := ChatdChild(response, "group")
	if !ok {
		return nil, errors.New("group response is missing its metadata")
	}
	id := shared.FirstNonEmpty(node.Attrs["id"], node.Attrs["jid"])
	if !strings.Contains(id, "@") && id != "" {
		id += "@g.us"
	}
	ref, err := normalizeGroupRef(id)
	if err != nil {
		return nil, err
	}
	group := &waappv1.GroupChat{GroupRef: ref, Name: node.Attrs["subject"]}
	for _, child := range wire.ChatdChildren(node) {
		switch child.Tag {
		case "participant":
			if code := groupParticipantErrorCode(child); code != "" && code != "0" {
				continue
			}
			member := groupMemberFromNode(child)
			if member.GetMemberRef() != "" {
				group.Members = append(group.Members, member)
			}
		case "description":
			body, _ := ChatdChild(child, "body")
			group.Description = ChatdNodeText(body)
			group.DescriptionId = child.Attrs["id"]
		case "locked":
			group.Locked = true
		case "announcement":
			group.AnnouncementOnly = true
		case "member_add_mode":
			group.AllMembersCanAdd = ChatdNodeText(child) == "all_member_add"
		case "membership_approval_mode":
			join, _ := ChatdChild(child, "group_join")
			group.JoinApproval = join.Attrs["state"] == "on"
		}
	}
	group.MemberCount = int32(len(group.Members))
	if count, err := strconv.ParseInt(node.Attrs["size"], 10, 32); err == nil && count > int64(group.MemberCount) {
		group.MemberCount = int32(count)
	}
	return group, nil
}

func groupMemberFromNode(node wire.ChatdNode) *waappv1.GroupMember {
	role := waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_MEMBER
	switch node.Attrs["type"] {
	case "admin":
		role = waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_ADMIN
	case "superadmin":
		role = waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_OWNER
	}
	return &waappv1.GroupMember{MemberRef: wacore.NormalizeWAJID(node.Attrs["jid"]), PhoneRef: wacore.NormalizeWAJID(node.Attrs["phone_number"]), Role: role}
}

package communication

import (
	"context"
	"errors"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func (c *Connection) UpdateGroup(ctx context.Context, request *waappv1.UpdateGroupRequest) error {
	_, err := groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (struct{}, error) {
		target, err := normalizeGroupRef(request.GetGroupRef())
		if err != nil {
			return struct{}{}, err
		}
		previousID := ""
		if _, ok := request.GetChange().(*waappv1.UpdateGroupRequest_Description); ok {
			group, err := s.read(ctx, target)
			if err != nil {
				return struct{}{}, err
			}
			previousID = group.GetDescriptionId()
		}
		node, err := groupChangeNode(request, previousID)
		if err != nil {
			return struct{}{}, err
		}
		_, err = s.query(ctx, s.request("set", target, node))
		return struct{}{}, err
	})
	return err
}

func groupChangeNode(request *waappv1.UpdateGroupRequest, previousID string) (wire.ChatdNode, error) {
	switch change := request.GetChange().(type) {
	case *waappv1.UpdateGroupRequest_Name:
		return wire.ChatdNode{Tag: "subject", Content: change.Name}, nil
	case *waappv1.UpdateGroupRequest_Description:
		attrs := map[string]string{}
		if previousID != "" {
			attrs["prev"] = previousID
		}
		var body []wire.ChatdNode
		if change.Description == "" {
			attrs["delete"] = "true"
		} else {
			attrs["id"] = NewTextProviderMessageID("")
			body = []wire.ChatdNode{{Tag: "body", Content: change.Description}}
		}
		return wire.ChatdNode{Tag: "description", Attrs: attrs, Content: body}, nil
	case *waappv1.UpdateGroupRequest_AnnouncementOnly:
		return groupFlag("announcement", "not_announcement", change.AnnouncementOnly), nil
	case *waappv1.UpdateGroupRequest_Locked:
		return groupFlag("locked", "unlocked", change.Locked), nil
	case *waappv1.UpdateGroupRequest_AllMembersCanAdd:
		mode := "admin_add"
		if change.AllMembersCanAdd {
			mode = "all_member_add"
		}
		return wire.ChatdNode{Tag: "member_add_mode", Content: mode}, nil
	case *waappv1.UpdateGroupRequest_JoinApproval:
		mode := "off"
		if change.JoinApproval {
			mode = "on"
		}
		return wire.ChatdNode{Tag: "membership_approval_mode", Content: []wire.ChatdNode{{Tag: "group_join", Attrs: map[string]string{"state": mode}}}}, nil
	default:
		return wire.ChatdNode{}, errors.New("group change is required")
	}
}

func groupFlag(on, off string, enabled bool) wire.ChatdNode {
	if enabled {
		return wire.ChatdNode{Tag: on}
	}
	return wire.ChatdNode{Tag: off}
}

package communication

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *groupSession) participantNodes(refs []string, action string) ([]wire.ChatdNode, error) {
	nodes := make([]wire.ChatdNode, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range shared.UniqueNonEmptyStrings(refs...) {
		jid, err := types.ParseJID(wacore.NormalizeWAJID(ref))
		if err != nil || jid.User == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "group member address is invalid", false)
		}
		target := signalIdentityForDevice(&s.current, jid.ToNonAD().String())
		if seen[target] {
			continue
		}
		seen[target] = true
		node := wire.ChatdNode{Tag: "participant", Attrs: map[string]string{"jid": target}}
		if action == "create" || action == "add" || action == "approve" {
			for _, hint := range s.current.ContactHints {
				hint = hint.Normalized()
				if hint.LIDJID == target && hint.PNJID != "" {
					node.Attrs["phone_number"] = hint.PNJID
					break
				}
			}
		}
		if action == "create" || action == "add" {
			token := PrivacyTokenForJID(s.current, target, s.connection.clock.Now())
			if len(token) == 0 {
				token = PrivacyTokenForJID(s.current, jid.ToNonAD().String(), s.connection.clock.Now())
			}
			if len(token) > 0 {
				node.Content = []wire.ChatdNode{{Tag: "privacy", Content: token}}
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func participantActionTag(action waappv1.GroupParticipantAction) (string, error) {
	switch action {
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_ADD:
		return "add", nil
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_REMOVE:
		return "remove", nil
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_PROMOTE:
		return "promote", nil
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_DEMOTE:
		return "demote", nil
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_APPROVE_JOIN:
		return "approve", nil
	case waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_REJECT_JOIN:
		return "reject", nil
	default:
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "group participant action is required", false)
	}
}

func (c *Connection) UpdateGroupParticipants(ctx context.Context, request *waappv1.UpdateGroupParticipantsRequest) ([]*waappv1.GroupParticipantResult, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) ([]*waappv1.GroupParticipantResult, error) {
		target, err := normalizeGroupRef(request.GetGroupRef())
		if err != nil {
			return nil, err
		}
		tag, err := participantActionTag(request.GetAction())
		if err != nil {
			return nil, err
		}
		nodes, err := s.participantNodes(request.GetMemberRefs(), tag)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return nil, errors.New("group members are required")
		}
		var results []*waappv1.GroupParticipantResult
		batches := [][]wire.ChatdNode{nodes}
		review := tag == "approve" || tag == "reject"
		if review {
			batches = make([][]wire.ChatdNode, len(nodes))
			for i, node := range nodes {
				batches[i] = []wire.ChatdNode{node}
			}
		}
		for _, batch := range batches {
			child := wire.ChatdNode{Tag: tag, Content: batch}
			if review {
				child = wire.ChatdNode{Tag: "membership_requests_action", Content: []wire.ChatdNode{child}}
			}
			response, err := s.query(ctx, s.request("set", target, child))
			if err != nil {
				return results, err
			}
			if review {
				response, _ = ChatdChild(response, "membership_requests_action")
			}
			result, ok := ChatdChild(response, tag)
			if !ok {
				return results, errors.New("group participant response is missing its action result")
			}
			results = append(results, groupParticipantResults(result, participantNodeRefs(batch))...)
		}
		return results, nil
	})
}

func groupParticipantResults(node wire.ChatdNode, requested []string) []*waappv1.GroupParticipantResult {
	results := make([]*waappv1.GroupParticipantResult, 0, len(requested))
	for _, ref := range requested {
		ref = wacore.NormalizeWAJID(ref)
		result := &waappv1.GroupParticipantResult{MemberRef: ref}
		found := false
		for _, child := range wire.ChatdChildren(node) {
			if child.Tag != "participant" {
				continue
			}
			member := groupMemberFromNode(child)
			if member.GetMemberRef() != ref && member.GetPhoneRef() != ref {
				continue
			}
			found = true
			result.MemberRef = member.GetMemberRef()
			code := groupParticipantErrorCode(child)
			if code != "" && code != "0" {
				result.Error = shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, fmt.Sprintf("group member operation was rejected (code %s)", code), false))
			}
			break
		}
		if !found {
			result.Error = shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "group membership confirmation is missing; refresh the member list", false))
		}
		results = append(results, result)
	}
	return results
}

func groupParticipantErrorCode(node wire.ChatdNode) string {
	if failure, ok := ChatdChild(node, "error"); ok {
		return shared.FirstNonEmpty(failure.Attrs["code"], "unknown")
	}
	return node.Attrs["error"]
}

func (c *Connection) ListGroupJoinRequests(ctx context.Context, ref string) ([]*waappv1.GroupJoinRequest, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) ([]*waappv1.GroupJoinRequest, error) {
		target, err := normalizeGroupRef(ref)
		if err != nil {
			return nil, err
		}
		response, err := s.query(ctx, s.request("get", target, wire.ChatdNode{Tag: "membership_approval_requests"}))
		if err != nil {
			return nil, err
		}
		return parseGroupJoinRequests(response)
	})
}

func parseGroupJoinRequests(response wire.ChatdNode) ([]*waappv1.GroupJoinRequest, error) {
	container, ok := ChatdChild(response, "membership_approval_requests")
	if !ok {
		return nil, errors.New("group join response is missing its request list")
	}
	var requests []*waappv1.GroupJoinRequest
	for _, node := range wire.ChatdChildren(container) {
		if node.Tag != "membership_approval_request" {
			continue
		}
		member := groupMemberFromNode(node)
		if member.GetMemberRef() == "" {
			return nil, errors.New("group join request is missing its member")
		}
		member.Role = waappv1.GroupMemberRole_GROUP_MEMBER_ROLE_UNSPECIFIED
		request := &waappv1.GroupJoinRequest{Member: member}
		if timestamp, err := strconv.ParseInt(node.Attrs["request_time"], 10, 64); err == nil && timestamp > 0 {
			stamp := timestamppb.New(time.Unix(timestamp, 0))
			if stamp.CheckValid() == nil {
				request.RequestedAt = stamp
			}
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func participantNodeRefs(nodes []wire.ChatdNode) []string {
	refs := make([]string, len(nodes))
	for i, node := range nodes {
		refs[i] = node.Attrs["jid"]
	}
	return refs
}

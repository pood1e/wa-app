package communication

import (
	"context"
	"errors"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type groupSession struct {
	connection *Connection
	current    state.NativeState
	changed    bool
}

func groupOperation[T any](ctx context.Context, c *Connection, work func(context.Context, *groupSession) (T, error)) (result T, err error) {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err = c.LockInteractiveIQLocked(ctx); err != nil {
		return result, err
	}
	defer c.UnlockInteractiveIQLocked()
	current, err := c.loadState(ctx, c.Input.ClientProfileID)
	if err != nil {
		return result, err
	}
	session := &groupSession{connection: c, current: current}
	result, err = work(ctx, session)
	if session.changed {
		checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, c.SaveState(checkpoint, c.Input.ClientProfileID, session.current))
	}
	return result, err
}

func (s *groupSession) query(ctx context.Context, request wire.ChatdNode) (wire.ChatdNode, error) {
	c := s.connection
	response, update, err := c.sendIQLocked(ctx, s.current, c.Input.RegisteredIdentityID, c.Input.AppVersion, request, "group request timed out")
	s.changed = ApplyChatdSessionUpdateState(&s.current, update) || s.changed
	if err == nil {
		err = ChatdIQError(response)
	}
	return response, err
}

func (s *groupSession) read(ctx context.Context, ref string) (*waappv1.GroupChat, error) {
	request, err := groupInfoIQ(ref, s.connection.ids.NewID("wa_group_"))
	if err != nil {
		return nil, err
	}
	response, err := s.query(ctx, request)
	if err != nil {
		return nil, err
	}
	return s.parse(response)
}

func (s *groupSession) parse(response wire.ChatdNode) (*waappv1.GroupChat, error) {
	group, err := parseGroupResponse(response)
	if err != nil {
		return nil, err
	}
	s.changed = ApplyChatdSessionUpdateState(&s.current, ChatdSessionUpdate{ContactHints: groupContactHints(group)}) || s.changed
	pn, identity := ownMessageJIDs(&s.current)
	for _, member := range group.GetMembers() {
		if member.GetMemberRef() == identity || member.GetMemberRef() == pn || member.GetPhoneRef() == pn {
			group.SelfMemberRef = member.GetMemberRef()
			break
		}
	}
	return group, nil
}

func (c *Connection) CreateGroup(ctx context.Context, request *waappv1.CreateGroupRequest) (*waappv1.GroupResponse, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (*waappv1.GroupResponse, error) {
		children, err := s.participantNodes(request.GetMemberRefs(), "create")
		if err != nil {
			return nil, err
		}
		response, err := s.query(ctx, s.request("set", "g.us", wire.ChatdNode{Tag: "create", Attrs: map[string]string{"subject": request.GetName()}, Content: children}))
		if err != nil {
			return nil, err
		}
		group, err := s.parse(response)
		if err != nil {
			return nil, err
		}
		node, _ := ChatdChild(response, "group")
		return &waappv1.GroupResponse{Group: group, ParticipantResults: groupParticipantResults(node, participantNodeRefs(children))}, nil
	})
}

func (c *Connection) GetGroup(ctx context.Context, ref string) (*waappv1.GroupChat, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (*waappv1.GroupChat, error) { return s.read(ctx, ref) })
}

func (c *Connection) ListJoinedGroups(ctx context.Context) ([]*waappv1.GroupChat, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) ([]*waappv1.GroupChat, error) {
		response, err := s.query(ctx, s.request("get", "g.us", wire.ChatdNode{Tag: "participating", Content: []wire.ChatdNode{{Tag: "participants"}, {Tag: "description"}}}))
		if err != nil {
			return nil, err
		}
		container, ok := ChatdChild(response, "groups")
		if !ok {
			return nil, errors.New("joined group response is missing its group list")
		}
		groups := make([]*waappv1.GroupChat, 0, len(wire.ChatdChildren(container)))
		for _, node := range wire.ChatdChildren(container) {
			if node.Tag != "group" {
				continue
			}
			group, err := s.parse(wire.ChatdNode{Content: []wire.ChatdNode{node}})
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		return groups, nil
	})
}

func (c *Connection) LeaveGroup(ctx context.Context, ref string) error {
	_, err := groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (struct{}, error) {
		target, err := normalizeGroupRef(ref)
		if err != nil {
			return struct{}{}, err
		}
		_, err = s.query(ctx, s.request("set", "g.us", wire.ChatdNode{Tag: "leave", Content: []wire.ChatdNode{{Tag: "group", Attrs: map[string]string{"id": target}}}}))
		return struct{}{}, err
	})
	return err
}

func groupIQ(id, kind, target string, content wire.ChatdNode) wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": id, "type": kind, "xmlns": "w:g2", "to": target}, Content: []wire.ChatdNode{content}}
}

func groupInfoIQ(ref, id string) (wire.ChatdNode, error) {
	target, err := normalizeGroupRef(ref)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	return groupIQ(id, "get", target, wire.ChatdNode{Tag: "query", Attrs: map[string]string{"request": "interactive"}}), nil
}

func groupContactHints(group *waappv1.GroupChat) []wacore.WAContactHint {
	var hints []wacore.WAContactHint
	for _, member := range group.GetMembers() {
		if strings.HasSuffix(member.GetMemberRef(), "@lid") && member.GetPhoneRef() != "" {
			hints = append(hints, wacore.WAContactHint{LIDJID: member.GetMemberRef(), PNJID: member.GetPhoneRef()})
		}
	}
	return hints
}

func (s *groupSession) request(kind, target string, content wire.ChatdNode) wire.ChatdNode {
	return groupIQ(s.connection.ids.NewID("wa_group_"), kind, target, content)
}

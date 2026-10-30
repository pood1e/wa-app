package communication

import (
	"context"
	"errors"
	"net/url"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const groupInvitePrefix = "https://chat.whatsapp.com/"

func groupInviteCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	invalid := func() (string, error) {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "a valid WhatsApp group invite link is required", false)
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "chat.whatsapp.com") || parsed.User != nil {
			return invalid()
		}
		value = strings.Trim(strings.TrimSpace(parsed.Path), "/")
	}
	if len(value) < 10 || len(value) > 128 {
		return invalid()
	}
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return invalid()
	}
	return value, nil
}

func (c *Connection) GetGroupInvite(ctx context.Context, request *waappv1.GetGroupInviteRequest) (string, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (string, error) {
		target, err := normalizeGroupRef(request.GetGroupRef())
		if err != nil {
			return "", err
		}
		kind := "get"
		if request.GetRegenerate() {
			kind = "set"
		}
		response, err := s.query(ctx, s.request(kind, target, wire.ChatdNode{Tag: "invite"}))
		if err != nil {
			return "", err
		}
		invite, ok := ChatdChild(response, "invite")
		if !ok {
			return "", errors.New("group invite response is missing its invite")
		}
		code, err := groupInviteCode(invite.Attrs["code"])
		if err != nil {
			return "", errors.New("group invite response has an invalid code")
		}
		return groupInvitePrefix + code, nil
	})
}

func (c *Connection) InspectGroupInvite(ctx context.Context, link string) (*waappv1.GroupChat, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (*waappv1.GroupChat, error) {
		code, err := groupInviteCode(link)
		if err != nil {
			return nil, err
		}
		response, err := s.query(ctx, s.request("get", "g.us", wire.ChatdNode{Tag: "invite", Attrs: map[string]string{"code": code}}))
		if err != nil {
			return nil, err
		}
		return parseGroupResponse(response)
	})
}

func (c *Connection) JoinGroup(ctx context.Context, link string) (*waappv1.JoinGroupResponse, error) {
	return groupOperation(ctx, c, func(ctx context.Context, s *groupSession) (*waappv1.JoinGroupResponse, error) {
		code, err := groupInviteCode(link)
		if err != nil {
			return nil, err
		}
		response, err := s.query(ctx, s.request("set", "g.us", wire.ChatdNode{Tag: "invite", Attrs: map[string]string{"code": code}}))
		if err != nil {
			return nil, err
		}
		if pending, ok := ChatdChild(response, "membership_approval_request"); ok {
			ref := shared.FirstNonEmpty(pending.Attrs["jid"], pending.Attrs["id"])
			target, err := normalizeGroupRef(ref)
			if err != nil {
				return nil, errors.New("group join acknowledgement has an invalid group address")
			}
			return &waappv1.JoinGroupResponse{Group: &waappv1.GroupChat{GroupRef: target}, PendingApproval: true}, nil
		}
		group, err := s.parse(response)
		if err != nil {
			return nil, err
		}
		return &waappv1.JoinGroupResponse{Group: group}, nil
	})
}

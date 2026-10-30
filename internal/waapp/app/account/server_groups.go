package account

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type groupHandler struct{ *serverCore }

type groupOperations interface {
	CreateGroup(context.Context, *waappv1.CreateGroupRequest) (*waappv1.GroupResponse, error)
	GetGroup(context.Context, string) (*waappv1.GroupChat, error)
	ListJoinedGroups(context.Context) ([]*waappv1.GroupChat, error)
	UpdateGroup(context.Context, *waappv1.UpdateGroupRequest) error
	UpdateGroupParticipants(context.Context, *waappv1.UpdateGroupParticipantsRequest) ([]*waappv1.GroupParticipantResult, error)
	GetGroupInvite(context.Context, *waappv1.GetGroupInviteRequest) (string, error)
	InspectGroupInvite(context.Context, string) (*waappv1.GroupChat, error)
	JoinGroup(context.Context, string) (*waappv1.JoinGroupResponse, error)
	LeaveGroup(context.Context, string) error
	ListGroupJoinRequests(context.Context, string) ([]*waappv1.GroupJoinRequest, error)
}

func (s *groupHandler) CreateGroup(ctx context.Context, request *waappv1.CreateGroupRequest) (*waappv1.GroupResponse, error) {
	name := strings.TrimSpace(request.GetName())
	members := shared.UniqueNonEmptyStrings(request.GetMemberRefs()...)
	if !validGroupText(name, 100) || name == "" || len(members) == 0 || len(members) > 1024 {
		return &waappv1.GroupResponse{Error: shared.ToProtoError(groupValidationError("a group name of 1–100 characters and 1–1024 members are required"))}, nil
	}
	runner, err := s.groupRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.GroupResponse{Error: shared.ToProtoError(err)}, nil
	}
	normalized := proto.Clone(request).(*waappv1.CreateGroupRequest)
	normalized.Name, normalized.MemberRefs = name, members
	response, err := runner.CreateGroup(ctx, normalized)
	if response == nil {
		response = &waappv1.GroupResponse{}
	}
	if response.GetGroup() != nil {
		err = errors.Join(err, s.saveGroupContacts(ctx, request.GetWaAccountId(), []*waappv1.GroupChat{response.GetGroup()}))
	}
	response.Error = shared.ToProtoError(err)
	return response, nil
}

func (s *groupHandler) GetGroup(ctx context.Context, request *waappv1.GetGroupRequest) (*waappv1.GroupResponse, error) {
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err != nil {
		return &waappv1.GroupResponse{Error: shared.ToProtoError(err)}, nil
	}
	group, err := runner.GetGroup(ctx, request.GetGroupRef())
	if err == nil && group != nil {
		err = s.saveGroupContacts(ctx, request.GetWaAccountId(), []*waappv1.GroupChat{group})
	}
	return &waappv1.GroupResponse{Group: group, Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) ListJoinedGroups(ctx context.Context, request *waappv1.ListJoinedGroupsRequest) (*waappv1.ListJoinedGroupsResponse, error) {
	runner, err := s.groupRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.ListJoinedGroupsResponse{Error: shared.ToProtoError(err)}, nil
	}
	groups, err := runner.ListJoinedGroups(ctx)
	if err == nil {
		err = s.saveGroupContacts(ctx, request.GetWaAccountId(), groups)
	}
	return &waappv1.ListJoinedGroupsResponse{Groups: groups, Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) UpdateGroup(ctx context.Context, request *waappv1.UpdateGroupRequest) (*waappv1.GroupMutationResponse, error) {
	if request != nil {
		request = proto.Clone(request).(*waappv1.UpdateGroupRequest)
	}
	if err := validateGroupChange(request); err != nil {
		return &waappv1.GroupMutationResponse{Error: shared.ToProtoError(err)}, nil
	}
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err == nil {
		err = runner.UpdateGroup(ctx, request)
	}
	if err == nil {
		if change, ok := request.GetChange().(*waappv1.UpdateGroupRequest_Name); ok {
			err = s.saveGroupContacts(ctx, request.GetWaAccountId(), []*waappv1.GroupChat{{GroupRef: request.GetGroupRef(), Name: change.Name}})
		}
	}
	return &waappv1.GroupMutationResponse{Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) UpdateGroupParticipants(ctx context.Context, request *waappv1.UpdateGroupParticipantsRequest) (*waappv1.GroupMutationResponse, error) {
	if len(request.GetMemberRefs()) == 0 || len(request.GetMemberRefs()) > 1024 || request.GetAction() == waappv1.GroupParticipantAction_GROUP_PARTICIPANT_ACTION_UNSPECIFIED {
		return &waappv1.GroupMutationResponse{Error: shared.ToProtoError(groupValidationError("a participant action and 1–1024 members are required"))}, nil
	}
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err != nil {
		return &waappv1.GroupMutationResponse{Error: shared.ToProtoError(err)}, nil
	}
	results, err := runner.UpdateGroupParticipants(ctx, request)
	return &waappv1.GroupMutationResponse{ParticipantResults: results, Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) LeaveGroup(ctx context.Context, request *waappv1.GetGroupRequest) (*waappv1.GroupMutationResponse, error) {
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err == nil {
		err = runner.LeaveGroup(ctx, request.GetGroupRef())
	}
	return &waappv1.GroupMutationResponse{Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) saveGroupContacts(ctx context.Context, accountID string, groups []*waappv1.GroupChat) error {
	contacts := make([]*waappv1.WAContact, 0, len(groups))
	now := s.clock.Now()
	for _, group := range groups {
		if group == nil {
			continue
		}
		contact := wamodel.ContactFromRef(accountID, group.GetGroupRef(), now, now)
		if contact == nil {
			continue
		}
		contact.DisplayName = group.GetName()
		contacts = append(contacts, contact)
	}
	return s.store.SaveWAContacts(ctx, contacts)
}

func (s *groupHandler) groupTargetRunner(ctx context.Context, accountID, ref string) (groupOperations, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasSuffix(ref, "@g.us") || strings.TrimSuffix(ref, "@g.us") == "" {
		return nil, groupValidationError("a group address is required")
	}
	return s.groupRunner(ctx, accountID)
}

func (s *groupHandler) groupRunner(ctx context.Context, ref string) (groupOperations, error) {
	id, err := wamodel.RequireWAAccountID(ref)
	if err != nil {
		return nil, err
	}
	if _, err = s.GetWAAccountRecord(ctx, id); err != nil {
		return nil, err
	}
	login, err := s.activeContactResolveLoginState(ctx, id)
	if err != nil {
		return nil, err
	}
	runner, err := s.longConnections.Acquire(ctx, login)
	if err != nil {
		return nil, err
	}
	groups, ok := runner.(groupOperations)
	if !ok {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "group operations are unavailable", false)
	}
	return groups, nil
}

func validGroupText(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func validateGroupChange(request *waappv1.UpdateGroupRequest) error {
	switch change := request.GetChange().(type) {
	case *waappv1.UpdateGroupRequest_Name:
		name := strings.TrimSpace(change.Name)
		if name == "" || !validGroupText(name, 100) {
			return groupValidationError("group name must be 1–100 characters")
		}
		change.Name = name
	case *waappv1.UpdateGroupRequest_Description:
		if !validGroupText(change.Description, 65536) {
			return groupValidationError("group description is invalid")
		}
	case *waappv1.UpdateGroupRequest_AnnouncementOnly, *waappv1.UpdateGroupRequest_Locked, *waappv1.UpdateGroupRequest_AllMembersCanAdd, *waappv1.UpdateGroupRequest_JoinApproval:
	default:
		return groupValidationError("one group change is required")
	}
	return nil
}

func groupValidationError(message string) error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, message, false)
}

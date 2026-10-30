package account

import (
	"context"
	"errors"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *groupHandler) GetGroupInvite(ctx context.Context, request *waappv1.GetGroupInviteRequest) (*waappv1.GroupInviteResponse, error) {
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err != nil {
		return &waappv1.GroupInviteResponse{Error: shared.ToProtoError(err)}, nil
	}
	link, err := runner.GetGroupInvite(ctx, request)
	return &waappv1.GroupInviteResponse{Link: link, Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) InspectGroupInvite(ctx context.Context, request *waappv1.GroupInviteRequest) (*waappv1.GroupResponse, error) {
	if request.GetLink() == "" {
		return &waappv1.GroupResponse{Error: shared.ToProtoError(groupValidationError("a group invite link is required"))}, nil
	}
	runner, err := s.groupRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.GroupResponse{Error: shared.ToProtoError(err)}, nil
	}
	group, err := runner.InspectGroupInvite(ctx, request.GetLink())
	return &waappv1.GroupResponse{Group: group, Error: shared.ToProtoError(err)}, nil
}

func (s *groupHandler) JoinGroup(ctx context.Context, request *waappv1.GroupInviteRequest) (*waappv1.JoinGroupResponse, error) {
	if request.GetLink() == "" {
		return &waappv1.JoinGroupResponse{Error: shared.ToProtoError(groupValidationError("a group invite link is required"))}, nil
	}
	runner, err := s.groupRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.JoinGroupResponse{Error: shared.ToProtoError(err)}, nil
	}
	response, err := runner.JoinGroup(ctx, request.GetLink())
	if response == nil {
		response = &waappv1.JoinGroupResponse{}
	}
	if response.GetGroup() != nil && !response.GetPendingApproval() {
		err = errors.Join(err, s.saveGroupContacts(ctx, request.GetWaAccountId(), []*waappv1.GroupChat{response.GetGroup()}))
	}
	response.Error = shared.ToProtoError(err)
	return response, nil
}

func (s *groupHandler) ListGroupJoinRequests(ctx context.Context, request *waappv1.GetGroupRequest) (*waappv1.ListGroupJoinRequestsResponse, error) {
	runner, err := s.groupTargetRunner(ctx, request.GetWaAccountId(), request.GetGroupRef())
	if err != nil {
		return &waappv1.ListGroupJoinRequestsResponse{Error: shared.ToProtoError(err)}, nil
	}
	requests, err := runner.ListGroupJoinRequests(ctx, request.GetGroupRef())
	return &waappv1.ListGroupJoinRequestsResponse{Requests: requests, Error: shared.ToProtoError(err)}, nil
}

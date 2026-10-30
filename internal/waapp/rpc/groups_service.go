package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type groupServiceServer struct {
	waappv1.UnimplementedWaGroupServiceServer
	application *account.Application
}

func (s *groupServiceServer) CreateGroup(ctx context.Context, req *waappv1.CreateGroupRequest) (*waappv1.GroupResponse, error) {
	return s.application.CreateGroup(ctx, req)
}
func (s *groupServiceServer) GetGroup(ctx context.Context, req *waappv1.GetGroupRequest) (*waappv1.GroupResponse, error) {
	return s.application.GetGroup(ctx, req)
}

func (s *groupServiceServer) ListJoinedGroups(ctx context.Context, req *waappv1.ListJoinedGroupsRequest) (*waappv1.ListJoinedGroupsResponse, error) {
	return s.application.ListJoinedGroups(ctx, req)
}
func (s *groupServiceServer) UpdateGroup(ctx context.Context, req *waappv1.UpdateGroupRequest) (*waappv1.GroupMutationResponse, error) {
	return s.application.UpdateGroup(ctx, req)
}
func (s *groupServiceServer) UpdateGroupParticipants(ctx context.Context, req *waappv1.UpdateGroupParticipantsRequest) (*waappv1.GroupMutationResponse, error) {
	return s.application.UpdateGroupParticipants(ctx, req)
}
func (s *groupServiceServer) GetGroupInvite(ctx context.Context, req *waappv1.GetGroupInviteRequest) (*waappv1.GroupInviteResponse, error) {
	return s.application.GetGroupInvite(ctx, req)
}
func (s *groupServiceServer) InspectGroupInvite(ctx context.Context, req *waappv1.GroupInviteRequest) (*waappv1.GroupResponse, error) {
	return s.application.InspectGroupInvite(ctx, req)
}
func (s *groupServiceServer) JoinGroup(ctx context.Context, req *waappv1.GroupInviteRequest) (*waappv1.JoinGroupResponse, error) {
	return s.application.JoinGroup(ctx, req)
}
func (s *groupServiceServer) LeaveGroup(ctx context.Context, req *waappv1.GetGroupRequest) (*waappv1.GroupMutationResponse, error) {
	return s.application.LeaveGroup(ctx, req)
}
func (s *groupServiceServer) ListGroupJoinRequests(ctx context.Context, req *waappv1.GetGroupRequest) (*waappv1.ListGroupJoinRequestsResponse, error) {
	return s.application.ListGroupJoinRequests(ctx, req)
}

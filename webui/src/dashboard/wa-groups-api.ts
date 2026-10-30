import type { GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import type { WaError } from '../proto/byte/v/forge/waapp/v1/common';
import type { CreateGroupRequest, GetGroupInviteRequest, GetGroupRequest, GroupInviteRequest, GroupInviteResponse, GroupMutationResponse, GroupResponse, JoinGroupResponse, ListGroupJoinRequestsResponse, ListJoinedGroupsResponse, UpdateGroupParticipantsRequest, UpdateGroupRequest } from '../proto/byte/v/forge/waapp/v1/groups';
import { api, getWaResponse, mutateWaResponse } from './wa-api';

export function createWaGroup(request: CreateGroupRequest, signal?: AbortSignal) {
  return api<GroupResponse>('/api/wa/groups', { method: 'POST', body: JSON.stringify(request), signal }).then(requireGroupAcknowledgement);
}
export function updateWaGroup(request: UpdateGroupRequest, signal?: AbortSignal) {
  return mutateWaResponse<GroupMutationResponse>('/api/wa/groups', { method: 'PATCH', body: JSON.stringify(request), signal });
}
export function updateWaGroupParticipants(request: UpdateGroupParticipantsRequest, signal?: AbortSignal) {
  return api<GroupMutationResponse>('/api/wa/groups/participants', { method: 'POST', body: JSON.stringify(request), signal }).then((response) => { if (response.error?.message && !response.participant_results?.length) throw new Error(response.error.message); return response; });
}
export function getWaGroupInvite(request: GetGroupInviteRequest, signal?: AbortSignal) {
  return mutateWaResponse<GroupInviteResponse>('/api/wa/groups/invite', { method: 'POST', body: JSON.stringify(request), signal });
}
export function inspectWaGroupInvite(request: GroupInviteRequest, signal?: AbortSignal) {
  return mutateWaResponse<GroupResponse>('/api/wa/groups/inspect-invite', { method: 'POST', body: JSON.stringify(request), signal });
}
export function joinWaGroup(request: GroupInviteRequest, signal?: AbortSignal) {
  return api<JoinGroupResponse>('/api/wa/groups/join', { method: 'POST', body: JSON.stringify(request), signal }).then(requireGroupAcknowledgement);
}
export function leaveWaGroup(request: GetGroupRequest, signal?: AbortSignal) {
  return mutateWaResponse<GroupMutationResponse>('/api/wa/groups/leave', { method: 'POST', body: JSON.stringify(request), signal });
}
export function syncWaGroups(accountID: string, signal?: AbortSignal) {
  return mutateWaResponse<ListJoinedGroupsResponse>('/api/wa/groups/sync', { method: 'POST', body: JSON.stringify({ wa_account_id: accountID }), signal });
}
export function getWaGroupRequests(accountID: string, groupRef: string, signal?: AbortSignal) {
  const params = new URLSearchParams({ wa_account_id: accountID, group_ref: groupRef });
  return getWaResponse<ListGroupJoinRequestsResponse>(`/api/wa/groups/requests?${params}`, signal);
}

function requireGroupAcknowledgement<Response extends { group?: GroupChat; error?: WaError }>(response: Response) {
  if (!response.group?.group_ref) throw new Error(response.error?.message || '未能确认群聊操作结果，请先同步群聊');
  return response;
}

import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { UpdateGroupParticipantsRequest, UpdateGroupRequest } from '../proto/byte/v/forge/waapp/v1/groups';
import { getWaGroup, waKeys } from './wa-api';
import { updateWaGroup, updateWaGroupParticipants } from './wa-groups-api';
import { useWaRequestSignal } from './wa-request-signal';

export type GroupChange = Omit<UpdateGroupRequest, 'context' | 'wa_account_id' | 'group_ref'>;
export type GroupMemberChange = Pick<UpdateGroupParticipantsRequest, 'member_refs' | 'action'>;

export function useWaGroup(accountID: string, groupRef: string, enabled: boolean) {
  return useQuery({ queryKey: waKeys.group(accountID, groupRef), queryFn: ({ signal }) => getWaGroup(accountID, groupRef, signal), enabled: Boolean(accountID && groupRef.endsWith('@g.us') && enabled), staleTime: 300000, retry: false });
}

export function useWaGroupUpdate(accountID: string, groupRef: string) {
  const client = useQueryClient();
  const signal = useWaRequestSignal();
  return useMutation({ mutationFn: (change: GroupChange) => updateWaGroup({ ...change, wa_account_id: accountID, group_ref: groupRef, context: undefined }, signal()), onSettled: () => invalidateWaGroup(client, accountID, groupRef) });
}

export function useWaGroupParticipants(accountID: string, groupRef: string) {
  const client = useQueryClient();
  const signal = useWaRequestSignal();
  return useMutation({ mutationFn: (change: GroupMemberChange) => updateWaGroupParticipants({ ...change, wa_account_id: accountID, group_ref: groupRef, context: undefined }, signal()), onSettled: () => invalidateWaGroup(client, accountID, groupRef) });
}

export function invalidateWaGroup(client: QueryClient, accountID: string, groupRef: string) {
  void client.invalidateQueries({ queryKey: waKeys.group(accountID, groupRef) });
  void client.invalidateQueries({ queryKey: waKeys.groupRequests(accountID, groupRef) });
  void client.invalidateQueries({ queryKey: waKeys.contacts(accountID) });
}

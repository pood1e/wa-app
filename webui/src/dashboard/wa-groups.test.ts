import { afterEach, describe, expect, it, vi } from 'vitest';
import { GroupMemberRole, GroupParticipantAction, type GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import { createWaGroup, joinWaGroup, updateWaGroup, updateWaGroupParticipants } from './wa-groups-api';
import { groupPermissions } from './wa-group-permissions';

afterEach(() => { vi.unstubAllGlobals(); });

function group(overrides: Partial<GroupChat> = {}): GroupChat {
  return { group_ref: 'fixture@g.us', name: 'Fixture', members: [{ member_ref: '100001@lid', phone_ref: '', role: GroupMemberRole.GROUP_MEMBER_ROLE_MEMBER }], self_member_ref: '100001@lid', member_count: 1, description: '', description_id: '', locked: false, announcement_only: false, all_members_can_add: false, join_approval: false, ...overrides };
}

describe('native group permissions', () => {
  it('requires confirmed membership before enabling group operations', () => {
    expect(groupPermissions()).toEqual({ member: false, admin: false, edit: false, add: false, send: false });
    expect(groupPermissions(group({ self_member_ref: '' })).send).toBe(false);
    expect(groupPermissions(group({ self_member_ref: '100002@lid' })).send).toBe(false);
  });
  it('applies announcement, edit and add restrictions to ordinary members', () => {
    expect(groupPermissions(group())).toEqual({ member: true, admin: false, edit: true, add: false, send: true });
    expect(groupPermissions(group({ locked: true, announcement_only: true, all_members_can_add: true }))).toEqual({ member: true, admin: false, edit: false, add: true, send: false });
  });
  it('preserves owner and admin capabilities and normalizes device suffixes', () => {
    for (const role of [GroupMemberRole.GROUP_MEMBER_ROLE_OWNER, GroupMemberRole.GROUP_MEMBER_ROLE_ADMIN]) {
      expect(groupPermissions(group({ self_member_ref: '100001:2@lid', locked: true, announcement_only: true, members: [{ member_ref: '100001@lid', phone_ref: '', role }] }))).toEqual({ member: true, admin: true, edit: true, add: true, send: true });
    }
  });
});

describe('native group submissions', () => {
  it('retains remote creation and join acknowledgements when local persistence fails', async () => {
    const response = { group: group(), error: { message: 'local storage unavailable' } };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(response)));
    vi.stubGlobal('fetch', fetch);
    const created = await createWaGroup({ wa_account_id: 'account', name: 'Fixture', member_refs: ['100001@lid'], context: undefined });
    expect(created).toMatchObject(response);
    fetch.mockResolvedValue(new Response(JSON.stringify(response)));
    await expect(joinWaGroup({ wa_account_id: 'account', link: 'SyntheticInviteCode0123', context: undefined })).resolves.toMatchObject(response);
    expect(fetch).toHaveBeenCalledTimes(2);
  });
  it('retains a pending-approval result without claiming membership', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ group: { group_ref: 'fixture@g.us' }, pending_approval: true }))));
    await expect(joinWaGroup({ wa_account_id: 'account', link: 'SyntheticInviteCode0123', context: undefined })).resolves.toMatchObject({ pending_approval: true });
  });
  it('rejects an unconfirmed join instead of automatically resubmitting', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { message: 'upstream rejected' } })));
    vi.stubGlobal('fetch', fetch);
    await expect(joinWaGroup({ wa_account_id: 'account', link: 'SyntheticInviteCode0123', context: undefined })).rejects.toThrow('upstream rejected');
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it('preserves successful member actions alongside a later failure', async () => {
    const response = { participant_results: [{ member_ref: '100001@lid' }], error: { message: 'later request failed' } };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(response))));
    await expect(updateWaGroupParticipants({ wa_account_id: 'account', group_ref: 'fixture@g.us', member_refs: ['100001@lid', '100002@lid'], action: GroupParticipantAction.GROUP_PARTICIPANT_ACTION_APPROVE_JOIN, context: undefined })).resolves.toEqual(response);
  });
  it('submits explicit false settings through the generated contract', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{}'));
    vi.stubGlobal('fetch', fetch);
    await updateWaGroup({ wa_account_id: 'account', group_ref: 'fixture@g.us', locked: false, context: undefined });
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ wa_account_id: 'account', group_ref: 'fixture@g.us', locked: false });
  });
});

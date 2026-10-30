import { GroupMemberRole, type GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import { canonicalContactRef } from './wa-contact-aliases';

export function groupPermissions(group?: GroupChat) {
  const self = group?.members?.find((member) => canonicalContactRef(member.member_ref) === canonicalContactRef(group.self_member_ref || ''));
  const admin = self?.role === GroupMemberRole.GROUP_MEMBER_ROLE_ADMIN || self?.role === GroupMemberRole.GROUP_MEMBER_ROLE_OWNER;
  const member = Boolean(self);
  return { member, admin, edit: member && (!group?.locked || admin), add: member && (group?.all_members_can_add || admin), send: member && (!group?.announcement_only || admin) };
}

export function groupMemberRoleLabel(role: GroupMemberRole) {
  return role === GroupMemberRole.GROUP_MEMBER_ROLE_OWNER ? '群主' : role === GroupMemberRole.GROUP_MEMBER_ROLE_ADMIN ? '管理员' : '';
}

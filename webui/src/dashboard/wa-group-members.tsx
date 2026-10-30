import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Loader2, RefreshCw, UserPlus } from 'lucide-react';
import { toast } from 'sonner';
import type { WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import { GroupMemberRole, GroupParticipantAction, type GroupChat, type GroupMember, type GroupMutationResponse } from '../proto/byte/v/forge/waapp/v1/groups';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { waKeys } from './wa-api';
import { createWaSenderLookup } from './wa-chat-senders';
import type { WaContact } from './wa-chat-model';
import { WaContactAvatar } from './wa-contact-avatar';
import { canonicalContactRef } from './wa-contact-aliases';
import { groupMemberRoleLabel, groupPermissions } from './wa-group-permissions';
import { getWaGroupRequests } from './wa-groups-api';
import { useWaGroupParticipants } from './wa-group-query';
import { WaMemberPicker } from './wa-member-picker';

export function WaGroupMembers({ accountID, group, records, contacts, disabled }: { accountID: string; group: GroupChat; records: WAContact[]; contacts: WaContact[]; disabled: boolean }) {
  const permissions = groupPermissions(group);
  const members = group.members || [];
  const [adding, setAdding] = useState(false);
  const update = useWaGroupParticipants(accountID, group.group_ref);
  const sender = createWaSenderLookup(records, group);
  const act = (member: GroupMember, action: GroupParticipantAction) => update.mutate({ member_refs: [member.member_ref], action }, { onSuccess: (response) => notifyParticipantResult(response, '成员已更新') });
  return <section className="grid gap-3 border-b px-5 py-5" aria-label="群成员">
    <div className="flex items-center justify-between"><h3 className="text-sm font-medium">{group.member_count || members.length} 位成员</h3>{permissions.add && <Button variant="ghost" size="sm" disabled={disabled || update.isPending} onClick={() => setAdding(true)}><UserPlus />添加成员</Button>}</div>
    {members.map((member) => {
      const self = canonicalContactRef(member.member_ref) === canonicalContactRef(group.self_member_ref || '');
      const label = sender(member.member_ref, true);
      const editable = permissions.admin && !self && member.role !== GroupMemberRole.GROUP_MEMBER_ROLE_OWNER;
      return <div key={member.member_ref} className="grid gap-1 rounded-lg py-2">
        <div className="flex items-center gap-3"><WaContactAvatar size="sm" contact={{ title: self ? '你' : label.name, kind: WAContactKind.WA_CONTACT_KIND_USER, profilePictureURL: label.pictureURL }} /><div className="min-w-0 flex-1"><p className="truncate text-sm">{self ? '你' : label.name}</p>{label.detail && <p className="truncate text-xs text-muted-foreground">{label.detail}</p>}</div><span className="text-xs text-primary">{groupMemberRoleLabel(member.role)}</span></div>
        {editable && <div className="flex justify-end gap-1"><Button size="sm" variant="ghost" disabled={disabled || update.isPending} onClick={() => act(member, member.role === GroupMemberRole.GROUP_MEMBER_ROLE_ADMIN ? GroupParticipantAction.GROUP_PARTICIPANT_ACTION_DEMOTE : GroupParticipantAction.GROUP_PARTICIPANT_ACTION_PROMOTE)}>{member.role === GroupMemberRole.GROUP_MEMBER_ROLE_ADMIN ? '取消管理员' : '设为管理员'}</Button><Button size="sm" variant="ghost" className="text-destructive" disabled={disabled || update.isPending} onClick={() => { if (window.confirm(`将 ${label.name} 移出群聊？`)) act(member, GroupParticipantAction.GROUP_PARTICIPANT_ACTION_REMOVE); }}>移出</Button></div>}
      </div>;
    })}
    {update.isPending && <p role="status" className="text-xs text-muted-foreground">正在更新成员…</p>}
    {update.error && <Alert variant="destructive"><AlertDescription>{update.error.message}</AlertDescription></Alert>}
    <Dialog open={adding} onOpenChange={setAdding}>{adding && <DialogContent><DialogHeader><DialogTitle>添加群成员</DialogTitle><DialogDescription>选择要加入群聊的联系人。</DialogDescription></DialogHeader><AddGroupMembers accountID={accountID} group={group} contacts={contacts} onDone={() => setAdding(false)} /></DialogContent>}</Dialog>
  </section>;
}

function AddGroupMembers({ accountID, group, contacts, onDone }: { accountID: string; group: GroupChat; contacts: WaContact[]; onDone: () => void }) {
  const [selected, setSelected] = useState<string[]>([]);
  const update = useWaGroupParticipants(accountID, group.group_ref);
  const submit = () => update.mutate({ member_refs: selected, action: GroupParticipantAction.GROUP_PARTICIPANT_ACTION_ADD }, { onSuccess: (response) => {
    if (notifyParticipantResult(response, '成员已添加')) onDone();
    else setSelected([]);
  } });
  return <div className="grid gap-4">
    <WaMemberPicker contacts={contacts} selected={selected} onChange={setSelected} exclude={(group.members || []).flatMap((member) => [member.member_ref, member.phone_ref])} disabled={update.isPending} />
    {update.error && <Alert variant="destructive"><AlertDescription>{update.error.message}</AlertDescription></Alert>}
    <DialogFooter><Button variant="outline" onClick={onDone}>取消</Button><Button disabled={update.isPending || !selected.length} onClick={submit}>{update.isPending && <Loader2 className="animate-spin" />}添加成员</Button></DialogFooter>
  </div>;
}

export function WaGroupJoinRequests({ accountID, group, records, disabled }: { accountID: string; group: GroupChat; records: WAContact[]; disabled: boolean }) {
  const requests = useQuery({ queryKey: waKeys.groupRequests(accountID, group.group_ref), queryFn: ({ signal }) => getWaGroupRequests(accountID, group.group_ref, signal), enabled: !disabled, retry: false });
  const update = useWaGroupParticipants(accountID, group.group_ref);
  const sender = createWaSenderLookup(records, { ...group, members: [...(group.members || []), ...(requests.data?.requests || []).flatMap((request) => request.member ? [request.member] : [])] });
  const review = (ref: string, approve: boolean) => update.mutate({ member_refs: [ref], action: approve ? GroupParticipantAction.GROUP_PARTICIPANT_ACTION_APPROVE_JOIN : GroupParticipantAction.GROUP_PARTICIPANT_ACTION_REJECT_JOIN }, { onSuccess: (response) => notifyParticipantResult(response, approve ? '已批准入群申请' : '已拒绝入群申请') });
  return <section className="grid gap-3 border-b px-5 py-5" aria-label="入群申请">
    <div className="flex items-center justify-between"><h3 className="text-sm font-medium">入群申请</h3><Button size="icon-sm" variant="ghost" aria-label="刷新入群申请" disabled={disabled || requests.isFetching} onClick={() => void requests.refetch()}><RefreshCw className={requests.isFetching ? 'animate-spin' : ''} /></Button></div>
    {requests.isLoading && <p className="text-xs text-muted-foreground">正在加载申请…</p>}
    {requests.data && !requests.data.requests?.length && <p className="text-xs text-muted-foreground">暂无待处理的申请</p>}
    {(requests.data?.requests || []).map((request) => {
      const member = request.member;
      if (!member) return null;
      const label = sender(member.member_ref, true);
      return <div key={member.member_ref} className="flex items-center gap-2"><WaContactAvatar size="xs" contact={{ title: label.name, kind: WAContactKind.WA_CONTACT_KIND_USER, profilePictureURL: label.pictureURL }} /><div className="min-w-0 flex-1"><p className="truncate text-sm">{label.name}</p>{request.requested_at && <p className="text-xs text-muted-foreground">{new Date(request.requested_at).toLocaleString('zh-CN')}</p>}</div><Button size="sm" variant="ghost" disabled={disabled || update.isPending} onClick={() => review(member.member_ref, false)}>拒绝</Button><Button size="sm" disabled={disabled || update.isPending} onClick={() => review(member.member_ref, true)}>批准</Button></div>;
    })}
    {(requests.error || update.error) && <Alert variant="destructive"><AlertDescription>{requests.error?.message || update.error?.message}</AlertDescription></Alert>}
  </section>;
}

function notifyParticipantResult(response: GroupMutationResponse, success: string) {
  const errors = (response.participant_results || []).flatMap((result) => result.error?.message ? [result.error.message] : []);
  if (response.error?.message) errors.push(response.error.message);
  if (errors.length) { toast.error([...new Set(errors)].join('；')); return false; }
  toast.success(success);
  return true;
}

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Info, Loader2, LogOut, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';
import { WAContactKind, type WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { GroupResponse } from '../proto/byte/v/forge/waapp/v1/groups';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet';
import { waKeys } from './wa-api';
import type { WaContact } from './wa-chat-model';
import { WaContactAvatar } from './wa-contact-avatar';
import { leaveWaGroup } from './wa-groups-api';
import { groupPermissions } from './wa-group-permissions';
import { invalidateWaGroup, useWaGroup } from './wa-group-query';
import { WaGroupInvite } from './wa-group-invite';
import { WaGroupJoinRequests, WaGroupMembers } from './wa-group-members';
import { WaGroupSettings } from './wa-group-settings';
import { useWaRequestSignal } from './wa-request-signal';

type GroupDetailsProps = { accountID: string; groupRef: string; records: WAContact[]; contacts: WaContact[]; online: boolean; pictureURL?: string };

export function WaGroupDetails(props: GroupDetailsProps) {
  const [open, setOpen] = useState(false);
  return <Sheet open={open} onOpenChange={setOpen}>
    <SheetTrigger asChild><Button size="icon" variant="ghost" className="rounded-full" aria-label="群资料" title="群资料"><Info className="size-5" /></Button></SheetTrigger>
    {open && <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-md"><SheetHeader><SheetTitle>群资料</SheetTitle><SheetDescription className="sr-only">查看成员、编辑群资料与管理入群申请。</SheetDescription></SheetHeader><GroupDetailsContent {...props} /></SheetContent>}
  </Sheet>;
}

function GroupDetailsContent({ accountID, groupRef, records, contacts, online, pictureURL }: GroupDetailsProps) {
  const query = useWaGroup(accountID, groupRef, online);
  const group = query.data?.group;
  const permissions = groupPermissions(group);
  const client = useQueryClient();
  const signal = useWaRequestSignal();
  const leave = useMutation({ mutationFn: () => leaveWaGroup({ wa_account_id: accountID, group_ref: groupRef, context: undefined }, signal()), onSuccess: () => {
    client.setQueryData<GroupResponse>(waKeys.group(accountID, groupRef), (current) => current?.group ? { ...current, group: { ...current.group, self_member_ref: '', member_count: Math.max(0, (current.group.member_count || current.group.members?.length || 0) - 1), members: (current.group.members || []).filter((member) => member.member_ref !== current.group?.self_member_ref) } } : current);
  }, onSettled: () => invalidateWaGroup(client, accountID, groupRef) });
  const disabled = !online || Boolean(query.error) || leave.isPending;
  return <>
    <div className="grid justify-items-center gap-2 border-b px-5 pb-5 text-center"><WaContactAvatar contact={{ title: group?.name || '群聊', kind: WAContactKind.WA_CONTACT_KIND_GROUP, profilePictureURL: pictureURL }} /><h2 className="text-xl font-medium">{group?.name || '群聊'}</h2><p className="text-xs text-muted-foreground">{group ? `${group.member_count || group.members?.length || 0} 位成员` : query.isLoading ? '正在加载群资料…' : '暂无群资料'}</p><Button size="sm" variant="ghost" disabled={!online || query.isFetching || leave.isPending} onClick={() => void query.refetch()}><RefreshCw className={query.isFetching ? 'animate-spin' : ''} />刷新群资料</Button></div>
    {!online && <p role="status" className="px-5 py-3 text-xs text-muted-foreground">连接恢复后可更新群资料。</p>}
    {query.error && <Alert variant="destructive" className="m-4 w-auto"><AlertDescription>{query.error.message}</AlertDescription></Alert>}
    {group && <>
      <WaGroupSettings accountID={accountID} group={group} disabled={disabled} />
      {permissions.admin && <WaGroupInvite accountID={accountID} groupRef={groupRef} disabled={disabled} />}
      {permissions.admin && group.join_approval && <WaGroupJoinRequests accountID={accountID} group={group} records={records} disabled={disabled} />}
      <WaGroupMembers accountID={accountID} group={group} records={records} contacts={contacts} disabled={disabled} />
      {permissions.member ? <div className="grid gap-2 p-5"><Button variant="ghost" className="justify-start text-destructive" disabled={disabled} onClick={() => { if (window.confirm('退出此群聊？历史消息仍会保留。')) leave.mutate(undefined, { onSuccess: () => toast.success('已退出群聊') }); }}>{leave.isPending ? <Loader2 className="animate-spin" /> : <LogOut />}退出群聊</Button>{leave.error && <Alert variant="destructive"><AlertDescription>{leave.error.message}</AlertDescription></Alert>}</div> : <p className="px-5 py-4 text-sm text-muted-foreground">你已不在此群聊中，历史消息仍可查看。</p>}
    </>}
  </>;
}

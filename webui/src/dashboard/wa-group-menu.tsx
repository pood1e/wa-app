import { useId, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useForm, useWatch } from 'react-hook-form';
import { useNavigate } from 'react-router';
import { Link2, Loader2, RefreshCw, UsersRound } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Alert, AlertDescription } from '@/components/ui/alert';
import type { CreateGroupRequest } from '../proto/byte/v/forge/waapp/v1/groups';
import { waKeys } from './wa-api';
import { createWaGroup, syncWaGroups } from './wa-groups-api';
import { waContactPath } from './wa-route-paths';
import { WaMemberPicker } from './wa-member-picker';
import { WaJoinGroupForm } from './wa-join-group';
import { useWaRequestSignal } from './wa-request-signal';
import type { WaContact } from './wa-chat-model';

export function WaGroupMenu({ accountID, selfRef, contacts, disabled }: { accountID: string; selfRef: string; contacts: WaContact[]; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<'menu' | 'create' | 'join'>('menu');
  const client = useQueryClient();
  const signal = useWaRequestSignal();
  const sync = useMutation({ mutationFn: () => syncWaGroups(accountID, signal()), onSettled: () => { void client.invalidateQueries({ queryKey: waKeys.contacts(accountID) }); } });
  const close = () => setOpen(false);
  return (
    <Dialog open={open} onOpenChange={(next) => { setOpen(next); if (next) setMode('menu'); }}>
      <DialogTrigger asChild><Button size="icon" variant="ghost" className="size-10 rounded-full text-muted-foreground" title="群聊操作" aria-label="群聊操作" disabled={disabled}><UsersRound className="size-5" /></Button></DialogTrigger>
      {open && <DialogContent>
        <DialogHeader><DialogTitle>{mode === 'create' ? '创建群聊' : mode === 'join' ? '通过链接加入群聊' : '群聊'}</DialogTitle><DialogDescription>{mode === 'create' ? '填写群名称并选择成员。' : mode === 'join' ? '先查看群资料，再确认加入。' : '创建、加入或同步当前账号的群聊。'}</DialogDescription></DialogHeader>
        {mode === 'menu' ? <div className="grid gap-2">
          <Button className="h-12 justify-start" variant="ghost" onClick={() => setMode('create')}><UsersRound />创建群聊</Button>
          <Button className="h-12 justify-start" variant="ghost" onClick={() => setMode('join')}><Link2 />通过链接加入</Button>
          <Button className="h-12 justify-start" variant="ghost" disabled={sync.isPending} onClick={() => sync.mutate(undefined, { onSuccess: (response) => { toast.success(`已同步 ${response.groups?.length || 0} 个群聊`); } })}>{sync.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}同步已加入的群聊</Button>
          {sync.error && <Alert variant="destructive"><AlertDescription>{sync.error.message}</AlertDescription></Alert>}
        </div> : mode === 'create' ? <CreateGroupForm accountID={accountID} contacts={contacts} selfRef={selfRef} onDone={close} /> : <WaJoinGroupForm accountID={accountID} onDone={close} />}
      </DialogContent>}
    </Dialog>
  );
}

function CreateGroupForm({ accountID, selfRef, contacts, onDone }: { accountID: string; selfRef: string; contacts: WaContact[]; onDone: () => void }) {
  const id = useId();
  const client = useQueryClient();
  const navigate = useNavigate();
  const signal = useWaRequestSignal();
  const form = useForm<Pick<CreateGroupRequest, 'name' | 'member_refs'>>({ defaultValues: { name: '', member_refs: [] } });
  const members = useWatch({ control: form.control, name: 'member_refs', defaultValue: [] });
  const mutation = useMutation({ mutationFn: (fields: Pick<CreateGroupRequest, 'name' | 'member_refs'>) => createWaGroup({ ...fields, wa_account_id: accountID, context: undefined }, signal()), onSettled: () => { void client.invalidateQueries({ queryKey: waKeys.contacts(accountID) }); } });
  const submit = form.handleSubmit((fields) => mutation.mutate(fields, { onSuccess: (response) => {
    const group = response.group;
    if (!group?.group_ref) return;
    const rejected = response.participant_results?.filter((result) => result.error).length || 0;
    if (response.error?.message) toast.warning(`群已创建，本地同步失败：${response.error.message}`);
    else if (rejected) toast.warning(`群已创建，${rejected} 位成员未确认加入，请查看群资料`);
    else toast.success('群聊已创建');
    onDone();
    navigate(waContactPath(accountID, group.group_ref));
  } }));
  return <form className="grid gap-4" onSubmit={submit}>
    <Field><FieldLabel htmlFor={id}>群名称</FieldLabel><Input id={id} required maxLength={100} autoComplete="off" disabled={mutation.isPending} {...form.register('name', { required: true })} /></Field>
    <WaMemberPicker contacts={contacts} selected={members} onChange={(refs) => form.setValue('member_refs', refs)} exclude={[selfRef]} disabled={mutation.isPending} />
    {mutation.error && <Alert variant="destructive"><AlertDescription>{mutation.error.message}</AlertDescription></Alert>}
    <DialogFooter><Button type="button" variant="outline" onClick={onDone}>取消</Button><Button type="submit" disabled={mutation.isPending || !members.length}>{mutation.isPending && <Loader2 className="animate-spin" />}创建群聊</Button></DialogFooter>
  </form>;
}

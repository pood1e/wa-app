import { useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { GroupChat, GroupInviteRequest } from '../proto/byte/v/forge/waapp/v1/groups';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { DialogFooter } from '@/components/ui/dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { WaContactAvatar } from './wa-contact-avatar';
import { useWaRequestSignal } from './wa-request-signal';
import { inspectWaGroupInvite, joinWaGroup } from './wa-groups-api';
import { waKeys } from './wa-api';
import { waContactPath } from './wa-route-paths';

export function WaJoinGroupForm({ accountID, onDone }: { accountID: string; onDone: () => void }) {
  const id = useId();
  const signal = useWaRequestSignal();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [preview, setPreview] = useState<{ link: string; group: GroupChat }>();
  const form = useForm<Pick<GroupInviteRequest, 'link'>>({ defaultValues: { link: '' } });
  const inspect = useMutation({ mutationFn: (link: string) => inspectWaGroupInvite({ wa_account_id: accountID, link, context: undefined }, signal()) });
  const join = useMutation({ mutationFn: (link: string) => joinWaGroup({ wa_account_id: accountID, link, context: undefined }, signal()), onSettled: () => { void client.invalidateQueries({ queryKey: waKeys.contacts(accountID) }); } });
  const pending = inspect.isPending || join.isPending;
  const submit = form.handleSubmit(({ link }) => {
    if (!preview || preview.link !== link) inspect.mutate(link, { onSuccess: (response) => { if (response.group) setPreview({ link, group: response.group }); } });
    else join.mutate(link, { onSuccess: (response) => {
      if (response.pending_approval) { toast.success('入群申请已提交，等待管理员确认'); onDone(); return; }
      const group = response.group;
      if (!group?.group_ref) return;
      if (response.error?.message) toast.warning(`已加入群聊，本地同步失败：${response.error.message}`);
      else toast.success('已加入群聊');
      onDone();
      navigate(waContactPath(accountID, group.group_ref));
    } });
  });
  return <form className="grid gap-4" onSubmit={submit}>
    <Field><FieldLabel htmlFor={id}>邀请链接</FieldLabel><Input id={id} type="url" placeholder="https://chat.whatsapp.com/…" required disabled={pending} {...form.register('link', { required: true, onChange: () => { setPreview(undefined); inspect.reset(); join.reset(); } })} /></Field>
    {preview && <div className="grid justify-items-center gap-2 rounded-xl bg-muted p-4 text-center"><WaContactAvatar contact={{ title: preview.group.name, kind: WAContactKind.WA_CONTACT_KIND_GROUP }} /><h3 className="font-semibold">{preview.group.name || '群聊'}</h3>{preview.group.member_count > 0 && <p className="text-xs text-muted-foreground">{preview.group.member_count} 位成员</p>}{preview.group.description && <p className="max-h-32 overflow-y-auto whitespace-pre-wrap text-sm text-muted-foreground">{preview.group.description}</p>}{preview.group.join_approval && <p className="text-xs text-primary">加入此群聊需要管理员批准</p>}</div>}
    {(inspect.error || join.error) && <Alert variant="destructive"><AlertDescription>{inspect.error?.message || join.error?.message}</AlertDescription></Alert>}
    <DialogFooter><Button type="button" variant="outline" onClick={onDone}>取消</Button><Button type="submit" disabled={pending}>{pending && <Loader2 className="animate-spin" />}{preview ? preview.group.join_approval ? '申请加入' : '加入群聊' : '查看群资料'}</Button></DialogFooter>
  </form>;
}

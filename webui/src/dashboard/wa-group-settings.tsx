import { useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { Switch } from 'radix-ui';
import { Loader2, Pencil } from 'lucide-react';
import { toast } from 'sonner';
import type { GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { groupPermissions } from './wa-group-permissions';
import { useWaGroupUpdate, type GroupChange } from './wa-group-query';

export function WaGroupSettings({ accountID, group, disabled }: { accountID: string; group: GroupChat; disabled: boolean }) {
  const permissions = groupPermissions(group);
  const update = useWaGroupUpdate(accountID, group.group_ref);
  const settings = [
    { field: 'announcement_only', label: '仅管理员可发送消息' },
    { field: 'locked', label: '仅管理员可编辑群资料' },
    { field: 'all_members_can_add', label: '允许所有成员添加成员' },
    { field: 'join_approval', label: '批准新成员' },
  ] as const;
  return <section className="grid gap-4 border-b px-5 py-5" aria-label="群设置">
    <GroupTextField accountID={accountID} groupRef={group.group_ref} field="name" label="群名称" value={group.name} disabled={disabled || !permissions.edit} />
    <GroupTextField accountID={accountID} groupRef={group.group_ref} field="description" label="群描述" value={group.description} disabled={disabled || !permissions.edit} />
    {permissions.admin && <div className="grid gap-4 border-t pt-4">
      {settings.map(({ field, label }) => <label key={field} className="flex items-center justify-between gap-4 text-sm"><span>{label}</span><Switch.Root checked={Boolean(group[field])} onCheckedChange={(checked) => update.mutate({ [field]: checked }, { onSuccess: () => toast.success('群设置已更新') })} disabled={disabled || update.isPending} aria-label={label} className="h-6 w-10 shrink-0 rounded-full bg-muted-foreground/30 p-0.5 transition-colors outline-offset-2 data-[state=checked]:bg-primary disabled:opacity-50"><Switch.Thumb className="block size-5 rounded-full bg-white shadow-sm transition-transform data-[state=checked]:translate-x-4" /></Switch.Root></label>)}
      {update.isPending && <p role="status" className="text-xs text-muted-foreground">正在保存设置…</p>}
      {update.error && <Alert variant="destructive"><AlertDescription>{update.error.message}</AlertDescription></Alert>}
    </div>}
  </section>;
}

function GroupTextField({ accountID, groupRef, field, label, value, disabled }: { accountID: string; groupRef: string; field: 'name' | 'description'; label: string; value: string; disabled: boolean }) {
  const [open, setOpen] = useState(false);
  return <div>
    <div className="flex items-center justify-between gap-3"><h3 className="text-xs text-muted-foreground">{label}</h3>{!disabled && <Button size="icon-sm" variant="ghost" aria-label={`编辑${label}`} onClick={() => setOpen(true)}><Pencil className="size-4" /></Button>}</div>
    <p className="mt-1 whitespace-pre-wrap break-words text-sm">{value || '暂无描述'}</p>
    <Dialog open={open} onOpenChange={setOpen}>{open && <DialogContent><DialogHeader><DialogTitle>编辑{label}</DialogTitle><DialogDescription>{field === 'name' ? '更新群聊中显示的名称。' : '向成员介绍这个群聊。'}</DialogDescription></DialogHeader><GroupTextForm accountID={accountID} groupRef={groupRef} field={field} label={label} value={value} onDone={() => setOpen(false)} /></DialogContent>}</Dialog>
  </div>;
}

function GroupTextForm({ accountID, groupRef, field, label, value, onDone }: { accountID: string; groupRef: string; field: 'name' | 'description'; label: string; value: string; onDone: () => void }) {
  const id = useId();
  const form = useForm<{ value: string }>({ defaultValues: { value } });
  const update = useWaGroupUpdate(accountID, groupRef);
  const submit = form.handleSubmit((fields) => {
    const change: GroupChange = field === 'name' ? { name: fields.value } : { description: fields.value };
    update.mutate(change, { onSuccess: () => { toast.success(`${label}已更新`); onDone(); } });
  });
  return <form className="grid gap-4" onSubmit={submit}>
    <Field><FieldLabel htmlFor={id}>{label}</FieldLabel>{field === 'name' ? <Input id={id} maxLength={100} required disabled={update.isPending} {...form.register('value', { required: true })} /> : <textarea id={id} rows={5} className="w-full resize-y rounded-lg border bg-background px-3 py-2 text-sm" disabled={update.isPending} {...form.register('value')} />}</Field>
    {update.error && <Alert variant="destructive"><AlertDescription>{update.error.message}</AlertDescription></Alert>}
    <DialogFooter><Button type="button" variant="outline" onClick={onDone}>取消</Button><Button type="submit" disabled={update.isPending}>{update.isPending && <Loader2 className="animate-spin" />}保存</Button></DialogFooter>
  </form>;
}

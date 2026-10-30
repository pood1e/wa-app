import { useId, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useNavigate } from 'react-router';
import { Loader2, MessageSquarePlus } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import type { SaveWAContactRequest } from '../proto/byte/v/forge/waapp/v1/contacts';
import { saveWaContact, waKeys } from './wa-api';
import { waContactPath } from './wa-route-paths';
import { useWaRequestSignal } from './wa-request-signal';

type ContactFields = Pick<SaveWAContactRequest, 'phone_number' | 'display_name'>;

export function WaNewContact({ accountID, disabled }: { accountID: string; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button size="icon" variant="ghost" className="size-10 rounded-full text-muted-foreground" title="添加联系人" aria-label="添加联系人" disabled={disabled}><MessageSquarePlus className="size-5" /></Button></DialogTrigger>
      {open && <NewContactForm key={accountID} accountID={accountID} onDone={() => setOpen(false)} />}
    </Dialog>
  );
}

function NewContactForm({ accountID, onDone }: { accountID: string; onDone: () => void }) {
  const id = useId();
  const navigate = useNavigate();
  const client = useQueryClient();
  const signal = useWaRequestSignal();
  const form = useForm<ContactFields>({ defaultValues: { phone_number: '', display_name: '' } });
  const mutation = useMutation({
    mutationFn: async (fields: ContactFields) => {
      const response = await saveWaContact({ ...fields, wa_account_id: accountID, context: undefined }, signal());
      if (!response.contact?.jid) throw new Error('未能获取联系人，请重试');
      return response.contact;
    },
    onSettled: () => { void client.invalidateQueries({ queryKey: waKeys.contacts(accountID) }); },
  });
  return (
    <DialogContent>
      <DialogHeader><DialogTitle>添加联系人</DialogTitle><DialogDescription>通过手机号添加到当前账号的联系人列表。</DialogDescription></DialogHeader>
      <form className="grid gap-4" onSubmit={form.handleSubmit((fields) => mutation.mutate(fields, { onSuccess: (contact) => { onDone(); navigate(waContactPath(accountID, contact.jid)); } }))}>
        <fieldset className="grid gap-4" disabled={mutation.isPending}>
          <Field><FieldLabel htmlFor={`${id}-phone`}>手机号</FieldLabel><Input id={`${id}-phone`} type="tel" autoComplete="tel" placeholder="+国家区号 手机号" maxLength={64} required {...form.register('phone_number', { required: true })} /><p className="text-xs text-muted-foreground">请包含国家区号，例如 +44。</p></Field>
          <Field><FieldLabel htmlFor={`${id}-name`}>姓名（可选）</FieldLabel><Input id={`${id}-name`} autoComplete="off" maxLength={120} {...form.register('display_name')} /></Field>
        </fieldset>
        {mutation.error && <Alert variant="destructive" role="alert"><AlertDescription>{mutation.error.message}</AlertDescription></Alert>}
        <DialogFooter><Button type="button" variant="outline" onClick={onDone}>取消</Button><Button type="submit" disabled={mutation.isPending}>{mutation.isPending && <Loader2 className="size-4 animate-spin" />}{mutation.isPending ? '正在查找…' : '添加并打开聊天'}</Button></DialogFooter>
      </form>
    </DialogContent>
  );
}

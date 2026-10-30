import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Copy, Link2, Loader2, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { getWaGroupInvite } from './wa-groups-api';
import { useWaRequestSignal } from './wa-request-signal';

export function WaGroupInvite({ accountID, groupRef, disabled }: { accountID: string; groupRef: string; disabled: boolean }) {
  const [link, setLink] = useState('');
  const signal = useWaRequestSignal();
  const invite = useMutation({ mutationFn: (regenerate: boolean) => getWaGroupInvite({ wa_account_id: accountID, group_ref: groupRef, regenerate, context: undefined }, signal()) });
  const load = (regenerate: boolean) => invite.mutate(regenerate, { onSuccess: (response) => { setLink(response.link); if (regenerate) toast.success('邀请链接已重置'); } });
  const copy = async () => {
    try { await navigator.clipboard.writeText(link); toast.success('邀请链接已复制'); }
    catch { toast.info('请选中下方链接复制'); }
  };
  return <section className="grid gap-3 border-b px-5 py-5" aria-label="群邀请链接">
    <h3 className="text-sm font-medium">邀请链接</h3>
    <p className="text-xs text-muted-foreground">与信任的人分享此链接。</p>
    {link && <Input value={link} readOnly aria-label="群邀请链接" onFocus={(event) => event.target.select()} />}
    <div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" disabled={disabled || invite.isPending} onClick={() => load(false)}>{invite.isPending ? <Loader2 className="animate-spin" /> : <Link2 />}获取链接</Button>{link && <Button variant="outline" size="sm" onClick={() => void copy()}><Copy />复制</Button>}<Button variant="ghost" size="sm" disabled={disabled || invite.isPending} onClick={() => { if (window.confirm('重置邀请链接？旧链接将失效。')) load(true); }}><RefreshCw />重置链接</Button></div>
    {invite.error && <Alert variant="destructive"><AlertDescription>{invite.error.message}</AlertDescription></Alert>}
  </section>;
}

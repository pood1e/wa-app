import { useRef, useState, type ChangeEvent, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ExternalLink, ImagePlus, Loader2, Monitor, RefreshCw, Unplug } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import type { LinkedDevice } from '../proto/byte/v/forge/waapp/v1/linked_devices';
import { linkWaDevice, listWaLinkedDevices, unlinkWaDevice } from './wa-linked-devices-api';

type Props = { accountID: string; online: boolean; onDone: (message: string) => void; onError: (message: string) => void };
const queryKey = (accountID: string) => ['wa', 'linked-devices', accountID];

export function WaLinkedDevices({ accountID, online, onDone, onError }: Props) {
  const cache = useQueryClient();
  const [qr, setQR] = useState('');
  const [reading, setReading] = useState(false);
  const [decodedImage, setDecodedImage] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const devices = useQuery({ queryKey: queryKey(accountID), queryFn: ({ signal }) => listWaLinkedDevices(accountID, signal), enabled: online, retry: false, staleTime: 30_000 });
  const reportError = (error: unknown) => onError(error instanceof Error ? error.message : '设备关联操作失败');
  const refresh = () => cache.invalidateQueries({ queryKey: queryKey(accountID) });
  const link = useMutation({
    mutationFn: () => linkWaDevice(accountID, qr.trim()), retry: false,
    onSuccess: (response) => {
      setQR(''); setDecodedImage(false);
      void refresh();
      if (response.device?.device_ref) onDone(response.error?.message ? '设备已关联，请刷新设备列表确认' : '主设备已授权，WhatsApp Web 正在初始化');
      else onError(response.outcome_unknown ? '关联结果尚未确认，请检查 WhatsApp Web 并刷新设备列表，保留当前 Web 页面' : response.error?.message || '服务端尚未确认设备关联');
    },
    onError: (error) => { setQR(''); setDecodedImage(false); reportError(error); void refresh(); },
  });
  const unlink = useMutation({
    mutationFn: (device: LinkedDevice) => unlinkWaDevice(accountID, device.device_ref), retry: false,
    onSuccess: () => { void refresh(); onDone('关联设备已退出登录'); }, onError: reportError,
  });
  const busy = reading || link.isPending || unlink.isPending;
  const readImage = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0]; event.currentTarget.value = '';
    if (!file) return;
    setReading(true); setQR(''); setDecodedImage(false);
    try {
      const { readPairingQRCode } = await import('./wa-pairing-qr');
      setQR(await readPairingQRCode(file)); setDecodedImage(true);
    } catch (error) { reportError(error); } finally { setReading(false); }
  };
  const submit = (event: FormEvent) => { event.preventDefault(); link.mutate(); };
  return (
    <div className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">{online ? '管理此账号的 Web 与桌面端登录' : '账号连接后可关联 WhatsApp Web'}</p>
        <Button size="icon-sm" variant="ghost" aria-label="刷新关联设备" disabled={!online || busy || devices.isFetching} onClick={() => void devices.refetch()}><RefreshCw className={devices.isFetching ? 'size-4 animate-spin' : 'size-4'} /></Button>
      </div>
      {devices.error ? <p role="alert" className="text-xs text-destructive">{devices.error.message}</p> : null}
      {devices.isPending && online ? <p role="status" className="text-xs text-muted-foreground">正在读取关联设备</p> : null}
      {online && devices.isSuccess && !devices.data.devices.length ? <p className="text-xs text-muted-foreground">暂无关联设备</p> : null}
      {devices.data?.devices.map((device) => <div key={device.device_ref} className="flex items-center gap-3 rounded-lg border px-3 py-2">
        <Monitor className="size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm">{device.name || device.platform || deviceLabel(device.device_ref)}</p>
          {device.linked_at ? <p className="text-xs text-muted-foreground">关联于 {new Date(device.linked_at).toLocaleString('zh-CN')}</p> : null}
        </div>
        <Dialog>
          <DialogTrigger asChild><Button size="icon-sm" variant="ghost" aria-label={`退出 ${device.name || deviceLabel(device.device_ref)}`} disabled={busy || !online}><Unplug className="size-4" /></Button></DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>退出此关联设备？</DialogTitle><DialogDescription>此设备的 WhatsApp 登录将被撤销，当前主设备账号保持登录。</DialogDescription></DialogHeader>
            <DialogFooter><DialogClose asChild><Button variant="outline">取消</Button></DialogClose><DialogClose asChild><Button onClick={() => unlink.mutate(device)}>退出设备</Button></DialogClose></DialogFooter>
          </DialogContent>
        </Dialog>
      </div>)}
      <form className="grid gap-3 border-t pt-3" onSubmit={submit}>
        <p className="text-xs leading-5 text-muted-foreground">打开 <a className="inline-flex items-center gap-1 text-primary underline underline-offset-2" href="https://web.whatsapp.com/" target="_blank" rel="noreferrer">WhatsApp Web<ExternalLink className="size-3" /></a>，上传该页面的二维码截图，或粘贴二维码内容。关联后该设备可读取和发送此账号的消息；首次登录同步账户设置与已有联系人资料。</p>
        <div className="flex flex-wrap items-center gap-2">
          <input ref={fileInput} type="file" accept="image/png,image/jpeg,image/webp" className="sr-only" aria-label="选择 Web 二维码截图" disabled={!online || busy} onChange={(event) => void readImage(event)} />
          <Button type="button" variant="outline" disabled={!online || busy} onClick={() => fileInput.current?.click()}>{reading ? <Loader2 className="size-4 animate-spin" /> : <ImagePlus className="size-4" />}上传二维码截图</Button>
          <Input type="password" className="min-w-48 flex-1" aria-label="WhatsApp Web 二维码内容" placeholder={decodedImage ? '已识别二维码，点击关联设备' : '粘贴二维码内容'} value={decodedImage ? '' : qr} maxLength={8192} autoComplete="off" spellCheck={false} disabled={!online || busy} onChange={(event) => { setQR(event.target.value); setDecodedImage(false); }} />
          <Button type="submit" disabled={!online || busy || !qr.trim()}>{link.isPending ? <><Loader2 className="size-4 animate-spin" />正在关联</> : '关联设备'}</Button>
        </div>
      </form>
    </div>
  );
}

function deviceLabel(ref: string) {
  return `设备 ${ref.split('@')[0].split(':')[1] || ''}`.trim();
}

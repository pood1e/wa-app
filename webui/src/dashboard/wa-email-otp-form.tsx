import { useEffect, useState } from 'react';
import { CheckCircle2, Eraser, Send } from 'lucide-react';
import type { AccountSettingsOperation } from '../proto/byte/v/forge/waapp/v1/account_settings';
import { remainingSeconds } from './wa-time';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

type Props = {
  operation?: AccountSettingsOperation;
  code: string;
  busy: boolean;
  onCodeChange: (value: string) => void;
  onRequest: () => void;
  onVerify: () => void;
};

export function WaEmailOtpForm({ operation, code, busy, onCodeChange, onRequest, onVerify }: Props) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const remaining = remainingSeconds(operation?.wait_time, operation?.completed_at, now);
  return (
    <div className="grid gap-2 border-t border-border pt-4">
      <div className="inline-flex items-center gap-2 text-sm font-medium"><Send size={15} />邮箱验证码</div>
      <div className="grid gap-2 sm:grid-cols-[auto_1fr_auto]">
        <Button type="button" variant="outline" disabled={busy || remaining > 0} onClick={onRequest}>
          <Send size={14} />{remaining > 0 ? `${remaining} 秒后重发` : '重新发送'}
        </Button>
        <div className="relative">
          <Input className="pr-10" value={code} onChange={(event) => onCodeChange(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={6} disabled={busy} placeholder="6 位验证码" aria-label="邮箱验证码" />
          <Button type="button" className="absolute right-1 top-1/2 -translate-y-1/2" size="icon-sm" variant="ghost" aria-label="清空验证码" title="清空验证码" disabled={busy || !code} onClick={() => onCodeChange('')}><Eraser size={14} /></Button>
        </div>
        <Button type="button" disabled={busy || !/^\d{6}$/.test(code)} onClick={onVerify}><CheckCircle2 size={14} />验证</Button>
      </div>
    </div>
  );
}

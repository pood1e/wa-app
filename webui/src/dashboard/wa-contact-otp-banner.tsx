import { Copy, ShieldCheck } from 'lucide-react';
import { toast } from 'sonner';
import type { OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import { Button } from '@/components/ui/button';

export function WaContactOtpBanner({ otp }: { otp?: OtpMessage }) {
  const code = otp?.otp?.value || otp?.otp?.redacted_value || '';
  if (!otp || !code) return null;
  return (
    <div className="rounded-2xl border border-primary/20 bg-primary/10 p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs font-medium text-primary">
            <ShieldCheck className="size-3.5" />
            账号转出 · 旧设备验证
          </div>
          <div className="mt-1 font-mono text-2xl font-semibold tracking-[0.28em] text-foreground">{formatOtpCode(code)}</div>
          <div className="mt-1 text-[11px] text-muted-foreground">在新设备 WhatsApp 输入此码完成转出 · {expiryText(otp.expires_at)}</div>
        </div>
        <Button className="shrink-0 rounded-full" type="button" size="icon" variant="secondary" title="复制验证码" aria-label="复制验证码" onClick={() => void copyOtp(code)}>
          <Copy className="size-4" />
        </Button>
      </div>
    </div>
  );
}

async function copyOtp(code: string) {
  try {
    await navigator.clipboard.writeText(code);
    toast.success('验证码已复制');
  } catch {
    toast.error('复制失败');
  }
}

function formatOtpCode(code: string) {
  const compact = code.replace(/\s+/g, '');
  return compact.length === 6 ? `${compact.slice(0, 3)} ${compact.slice(3)}` : compact;
}

function expiryText(value?: string) {
  const expiresAt = Date.parse(value || '');
  if (!Number.isFinite(expiresAt)) return '等待过期时间';
  const remaining = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000));
  return remaining > 0 ? `${remaining} 秒后过期` : '已过期';
}

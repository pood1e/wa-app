import { useState } from 'react';
import { PhoneForwarded } from 'lucide-react';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { Button } from '@/components/ui/button';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { resolveWaPhoneTarget } from './wa-utils';

type Props = {
  account: WAAccount;
  busy?: boolean;
  onError: (message: string) => void;
};

export function WaAccountChangeNumberCard({ account, busy, onError }: Props) {
  const [countryCallingCode, setCountryCallingCode] = useState('');
  const [phone, setPhone] = useState('');
  const currentPhone = account.phone?.e164_number || '-';
  function startChangeNumber() {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入新手机号和国家拨号码');
    return onError('换绑手机号链路尚未接入：需要按 APK ChangeNumber/ChangeNumberOverview 链路补齐后端实现');
  }
  return (
    <div className="grid gap-3">
      <div className="text-xs text-muted-foreground">对应已登录账号安全设置里的 Change number，不是注册侧旧设备验证。</div>
      <FieldGroup>
        <Field>
          <FieldLabel>当前手机号</FieldLabel>
          <Input readOnly value={currentPhone} />
        </Field>
        <Field>
          <FieldLabel>新国家拨号码</FieldLabel>
          <Input placeholder="+1" value={countryCallingCode} onChange={(event) => setCountryCallingCode(event.target.value)} disabled={busy} />
        </Field>
        <Field>
          <FieldLabel>新手机号</FieldLabel>
          <Input placeholder="4155550123" value={phone} onChange={(event) => setPhone(event.target.value)} disabled={busy} />
        </Field>
        <Button type="button" disabled={busy} onClick={startChangeNumber}><PhoneForwarded size={14} />发起换绑</Button>
      </FieldGroup>
    </div>
  );
}

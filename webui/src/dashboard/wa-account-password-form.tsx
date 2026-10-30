import { type FormEvent, useState } from 'react';
import { KeyRound } from 'lucide-react';
import { isAccountPassword } from './wa-account-security-model';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';

type Props = {
  password: string;
  busy: boolean;
  configured: boolean;
  onPasswordChange: (value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
};

export function WaAccountPasswordForm({ password, busy, configured, onPasswordChange, onSubmit }: Props) {
  const [confirmation, setConfirmation] = useState('');
  return (
    <form className="grid gap-3" onSubmit={onSubmit}>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="wa-security-password">{configured ? '新密码' : '密码'}</FieldLabel>
          <Input id="wa-security-password" value={password} onChange={(event) => onPasswordChange(event.target.value)} autoComplete="new-password" type="password" maxLength={20} disabled={busy} autoFocus />
        </Field>
        <Field>
          <FieldLabel htmlFor="wa-security-password-confirmation">确认密码</FieldLabel>
          <Input id="wa-security-password-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="new-password" type="password" maxLength={20} disabled={busy} />
        </Field>
      </FieldGroup>
      <p className="text-xs text-muted-foreground">8–20 个字符，包含字母和数字。</p>
      <Button type="submit" className="w-full" disabled={busy || !isAccountPassword(password) || password !== confirmation}><KeyRound size={14} />{configured ? '修改密码' : '设置密码'}</Button>
    </form>
  );
}

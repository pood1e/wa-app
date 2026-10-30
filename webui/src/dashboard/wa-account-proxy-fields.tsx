import { useId } from 'react';
import { Button } from '@/components/ui/button';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { AccountProxyProtocol, type AccountProxy } from '../proto/byte/v/forge/waapp/v1/account_proxy';

type Props = {
  value: AccountProxy | undefined;
  stored?: AccountProxy;
  disabled?: boolean;
  defaultLabel?: string;
  onChange: (value: AccountProxy | undefined) => void;
};

export function WaAccountProxyFields({ value, stored, disabled, defaultLabel = '使用全局默认', onChange }: Props) {
  const id = useId();
  const hasPassword = Boolean(stored?.password?.redacted_value && value && stored.protocol === value.protocol && stored.host === value.host && stored.port === value.port && stored.username === value.username);
  const mode = value?.protocol || 'default';
  const patch = (next: Partial<AccountProxy>) => { if (value) onChange({ ...value, ...next }); };
  return (
    <fieldset className="grid gap-3" disabled={disabled}>
      <Field>
        <FieldLabel htmlFor={`${id}-protocol`}>账号代理</FieldLabel>
        <Select value={mode} disabled={disabled} onValueChange={(protocol) => onChange(protocol === 'default' ? undefined : {
          host: '', port: 0, username: '', password: undefined, ...value, protocol: protocol as AccountProxyProtocol,
        })}>
          <SelectTrigger className="w-full" id={`${id}-protocol`}><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="default">{defaultLabel}</SelectItem>
            <SelectItem value={AccountProxyProtocol.ACCOUNT_PROXY_PROTOCOL_HTTP}>自定义 HTTP 代理</SelectItem>
            <SelectItem value={AccountProxyProtocol.ACCOUNT_PROXY_PROTOCOL_SOCKS5}>自定义 SOCKS5 代理</SelectItem>
          </SelectContent>
        </Select>
      </Field>
      {value && <>
        <div className="grid grid-cols-[minmax(0,1fr)_100px] gap-3">
          <Field><FieldLabel htmlFor={`${id}-host`}>地址</FieldLabel><Input id={`${id}-host`} autoComplete="off" value={value.host} placeholder="代理域名或 IP" required onChange={(event) => patch({ host: event.target.value })} /></Field>
          <Field><FieldLabel htmlFor={`${id}-port`}>端口</FieldLabel><Input id={`${id}-port`} type="number" inputMode="numeric" min={1} max={65535} value={value.port || ''} required onChange={(event) => patch({ port: Number(event.target.value) })} /></Field>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field><FieldLabel htmlFor={`${id}-username`}>用户名（可选）</FieldLabel><Input id={`${id}-username`} autoComplete="off" maxLength={255} value={value.username} onChange={(event) => patch({ username: event.target.value, password: undefined })} /></Field>
          <Field><FieldLabel htmlFor={`${id}-password`}>密码（可选）</FieldLabel><Input id={`${id}-password`} type="password" autoComplete="new-password" maxLength={255} value={value.password?.value || ''} placeholder={hasPassword && value.password === undefined ? '留空保留已保存密码' : '代理密码'} onChange={(event) => patch({ password: { value: event.target.value, redacted_value: '', secret_ref: '' } })} /></Field>
        </div>
        {hasPassword && <Button className="justify-self-start" type="button" size="sm" variant="ghost" onClick={() => patch({ password: { value: '', redacted_value: '', secret_ref: '' } })}>清除已保存密码</Button>}
      </>}
    </fieldset>
  );
}

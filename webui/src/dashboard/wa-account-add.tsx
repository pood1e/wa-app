import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router';
import { ArrowRight } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { ClientPlatform } from '../proto/byte/v/forge/waapp/protocol/v1/account';
import { AccountAddStatus, type AccountAddFlow, type AccountAddResponse } from '../proto/byte/v/forge/waapp/v1/account_add';
import type { AccountProxy } from '../proto/byte/v/forge/waapp/v1/account_proxy';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { VerificationDeliveryMethod } from '../proto/byte/v/forge/waapp/v1/registration';
import { beginAccountAdd, cancelAccountAdd, deliverAccountAdd, getAccountAdd, requestAccountAddCode, submitAccountAddCode, submitAccountAddPassword } from './wa-account-add-api';
import { addingError, addingStatus, canRecoverRegistration, canSubmitCode, canSubmitPassword, channelState, isTransferMethod } from './wa-account-add-state';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaRegistrationOtpCard } from './wa-registration-otp-card';
import { WaAccountProxyFields } from './wa-account-proxy-fields';
import { validAccountProxy } from './wa-account-proxy-model';
import { resolveWaPhoneTarget } from './wa-utils';

type Props = { account?: WAAccount; disabled?: boolean; onChanged: () => void | Promise<void>; onDone: (message: string) => void; onError: (message: string) => void };
const methods = [
  { value: VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_SMS, label: '短信' },
  { value: VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_VOICE, label: '语音' },
  { value: VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_WA_OLD, label: '旧设备验证码' },
  { value: VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_EMAIL_OTP, label: '邮箱验证码' },
  { value: VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_SEND_SMS, label: '发送短信验证' },
];

export function WaAccountAdd({ account, disabled, onChanged, onDone, onError }: Props) {
  const [params, setParams] = useSearchParams();
  const flowID = params.get('flow') || '';
  const [flow, setFlow] = useState<AccountAddFlow | null>(null);
  const [phone, setPhone] = useState(account?.phone?.national_number || '');
  const [callingCode, setCallingCode] = useState(account?.phone?.country_calling_code || '');
  const [otp, setOtp] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [transfer, setTransfer] = useState(false);
  const [platform, setPlatform] = useState(ClientPlatform.CLIENT_PLATFORM_ANDROID);
  const [proxy, setProxy] = useState<AccountProxy | undefined>();
  const [now, setNow] = useState(Date.now());
  const loginNumber = account?.phone?.e164_number;
  const waiting = canSubmitCode(flow);
  const waitingPassword = canSubmitPassword(flow);
  const passwordWait = Math.max(0, Math.ceil((new Date(flow?.password_challenge?.next_attempt_at || 0).getTime() - now) / 1000));
  const transferChallenge = waiting && flow?.verification?.delivery_method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER;
  const recoveryPending = canRecoverRegistration(flow);
  const deliveryPending = flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_READY_TO_DELIVER;
  const accepted = flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_ACCEPTED;
  const ended = accepted || flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_CANCELLED || flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_EXPIRED;
  const locked = Boolean(flow && !ended && flow.status !== AccountAddStatus.ACCOUNT_ADD_STATUS_READY);
  const resolved = resolveWaPhoneTarget(phone, callingCode);
  const samePhone = Boolean(flow && resolved.target?.e164 === flow.phone?.e164_number && flow.platform === platform);
  const noTransferChannel = transfer && samePhone && flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_READY && methods.filter((method) => isTransferMethod(method.value)).every((method) => {
    const state = channelState(flow, method.value, now);
    return !state.available && state.remaining === 0;
  });

  useEffect(() => {
    if (!flowID) return undefined;
    const controller = new AbortController();
    getAccountAdd(flowID, controller.signal).then((response) => {
      if (!response.flow) return onError(response.error?.message || '本次添加已过期，请重新开始');
      if (loginNumber && response.flow.phone?.e164_number !== loginNumber) return onError('本次验证与当前账号不匹配，请重新开始');
      setFlow(response.flow);
      setPassword('');
      setPhone(response.flow.phone?.national_number || '');
      setCallingCode(response.flow.phone?.country_calling_code || '');
      setTransfer(Boolean(response.flow.source?.transfer));
      setPlatform(response.flow.platform || ClientPlatform.CLIENT_PLATFORM_ANDROID);
    }).catch((error: unknown) => { if (!controller.signal.aborted) onError(error instanceof Error ? error.message : '读取添加进度失败'); });
    return () => controller.abort();
  }, [flowID, loginNumber, onError]);

  useEffect(() => {
    if (!flow || ended) return undefined;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [flow, ended]);

  async function run(operation: () => Promise<AccountAddResponse>) {
    setBusy(true);
    try {
      const response = await operation();
      if (response.flow) {
        if (response.flow.verification?.verification_request_id !== flow?.verification?.verification_request_id || response.error?.code === WaErrorCode.WA_ERROR_CODE_VERIFICATION_REQUIRED || response.flow.password_challenge) setOtp('');
        if (response.flow.flow_id !== flow?.flow_id || !response.flow.password_challenge) setPassword('');
        setFlow(response.flow);
      }
      if (response.flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_ACCEPTED) {
        setOtp('');
        setProxy(undefined);
        onDone(account ? '已重新登录' : '账号已添加');
        await onChanged();
      }
      if (response.error) onError(addingError(response.flow || null, Date.now()) || response.error.message || '添加操作失败');
      return response;
    } catch (error) {
      onError(error instanceof Error ? error.message : '添加操作失败');
      return undefined;
    } finally { setBusy(false); }
  }

  async function probe() {
    if (!resolved.target) return onError(resolved.error || '请输入手机号');
    if (!validAccountProxy(proxy)) return onError('请填写有效的代理地址和端口');
    const response = await run(async () => {
      if (flow && !ended) {
        const cancelled = await cancelAccountAdd(flow.flow_id || '');
        if (cancelled.error) return cancelled;
      }
      return beginAccountAdd({ phone: resolved.target!.input, platform, source: transfer ? { transfer: {} } : { registration: {} }, proxy });
    });
    if (response?.flow?.flow_id) setParams({ flow: response.flow.flow_id }, { replace: true });
  }

  async function changeSource(value: boolean) {
    if (await cancel()) setTransfer(value);
  }

  async function cancel() {
    if (flow?.flow_id && !ended) {
      const response = await run(() => cancelAccountAdd(flow.flow_id || ''));
      if (!response || response.error) return false;
    }
    setFlow(null); setOtp(''); setPassword(''); setParams({}, { replace: true });
    return true;
  }

  const remaining = flow?.expires_at ? Math.max(0, Math.ceil((new Date(flow.expires_at).getTime() - now) / 60000)) : 0;
  return (
    <Card className="gap-7 rounded-2xl py-8 ring-border">
      <CardHeader className="justify-items-center gap-3 px-6 text-center sm:px-8">
        <span className="mb-1 grid size-14 place-items-center rounded-full bg-accent"><WhatsAppIcon className="size-8" /></span>
        <CardTitle className="text-xl font-medium">{accepted ? (account ? '已重新登录' : '账号已添加') : (account ? '重新登录 WhatsApp' : '验证你的手机号')}</CardTitle>
        <CardDescription className="max-w-xs leading-6">{account ? '验证身份以恢复登录，账号资料、联系人和历史消息会保留。' : '使用手机号注册 WhatsApp，或从旧设备转入已有账号。'}</CardDescription>
        {flow || busy ? <Badge className="rounded-full" variant={accepted ? 'default' : 'secondary'}>{busy ? '处理中' : addingStatus(flow)}</Badge> : null}
      </CardHeader>
      <CardContent className="grid gap-5 px-6 sm:px-8">
        {!account && <div role="group" aria-label="添加方式" className="flex gap-1 rounded-full bg-muted p-1">
          <Button className={`h-10 flex-1 rounded-full ${!transfer ? 'bg-card text-foreground shadow-sm hover:bg-card' : 'text-muted-foreground'}`} variant="ghost" aria-pressed={!transfer} disabled={busy || disabled || locked} onClick={() => void changeSource(false)}>注册账号</Button>
          <Button className={`h-10 flex-1 rounded-full ${transfer ? 'bg-card text-foreground shadow-sm hover:bg-card' : 'text-muted-foreground'}`} variant="ghost" aria-pressed={transfer} disabled={busy || disabled || locked} onClick={() => void changeSource(true)}>从旧设备转入</Button>
        </div>}
        <div role="group" aria-label="设备类型" className="flex items-center gap-2"><span className="mr-auto text-xs text-muted-foreground">设备类型</span>{[
          { value: ClientPlatform.CLIENT_PLATFORM_ANDROID, label: 'Android' },
          { value: ClientPlatform.CLIENT_PLATFORM_IOS, label: 'iOS' },
        ].map((option) => <Button className="h-8 rounded-full px-4" key={option.value} size="sm" aria-pressed={platform === option.value} variant={platform === option.value ? 'secondary' : 'ghost'} disabled={busy || disabled || locked} onClick={() => setPlatform(option.value)}>{option.label}</Button>)}</div>
        <FieldGroup>
          <div className="grid grid-cols-[100px_minmax(0,1fr)] gap-3">
            <Field><FieldLabel htmlFor="wa-calling-code">区号</FieldLabel><Input className="h-11" id="wa-calling-code" inputMode="tel" autoComplete="tel-country-code" value={callingCode} placeholder="+44" onChange={(event) => setCallingCode(event.target.value)} disabled={busy || disabled || locked || Boolean(account)} /></Field>
            <Field><FieldLabel htmlFor="wa-phone-number">手机号</FieldLabel><Input className="h-11" id="wa-phone-number" inputMode="tel" autoComplete="tel-national" value={phone} placeholder="输入手机号" onChange={(event) => setPhone(event.target.value)} disabled={busy || disabled || locked || Boolean(account)} /></Field>
          </div>
        </FieldGroup>
        {!accepted && <details className="rounded-xl border border-border px-4 py-3">
          <summary className="cursor-pointer text-sm text-muted-foreground">代理设置（可选）</summary>
          <div className="grid gap-3 pt-4">
            {flow && !ended ? <p className="text-xs leading-5 text-muted-foreground">本次添加使用开始时选定的代理。需要修改时，请先结束本次添加。</p> : <>
              <p className="text-xs leading-5 text-muted-foreground">已有账号自动沿用保存的代理；其他情况使用全局默认。自定义代理也会用于添加后的连接。</p>
              <WaAccountProxyFields value={proxy} disabled={busy || disabled} defaultLabel="自动选择" onChange={setProxy} />
            </>}
          </div>
        </details>}
        {!locked && !accepted && !samePhone ? <Button className="h-11 w-full rounded-full" disabled={busy || disabled || !resolved.target || !validAccountProxy(proxy)} aria-label="检测验证通道" onClick={() => void probe()}>下一步<ArrowRight className="size-4" /></Button> : null}
        {!locked && !accepted && samePhone && <div className="grid gap-2 sm:grid-cols-2">{methods.filter((method) => isTransferMethod(method.value) === transfer).map((method) => {
          const state = channelState(samePhone ? flow : null, method.value, now);
          return <Button className="h-10 rounded-full" key={method.value} variant="outline" disabled={busy || disabled || !state.available || flow?.status !== AccountAddStatus.ACCOUNT_ADD_STATUS_READY} onClick={() => void run(() => requestAccountAddCode(flow?.flow_id || '', method.value))}>{method.label}{state.remaining > 0 ? ` · ${state.remaining} 秒` : ''}</Button>;
        })}</div>}
        {transfer && !waiting && !accepted && <p className="text-center text-xs leading-5 text-muted-foreground">请保持旧设备上的 WhatsApp 在线，以接收转移验证码。</p>}
        {noTransferChannel && <p role="status" className="text-sm text-muted-foreground">该号码当前没有可用的旧设备验证通道，可返回注册账号使用短信或语音验证。</p>}
        {waiting && !transferChallenge && <WaRegistrationOtpCard value={otp} length={flow?.verification?.expected_code_length || 6} method={flow?.verification?.delivery_method} busy={busy || disabled} onChange={setOtp} onSubmit={() => void run(() => submitAccountAddCode(flow?.flow_id || '', otp))} />}
        {waitingPassword && <form className="grid gap-4 rounded-xl border border-border p-5" onSubmit={(event) => { event.preventDefault(); if (!busy && !disabled && password && passwordWait === 0) void run(() => submitAccountAddPassword(flow?.flow_id || '', password)); }}>
          <p className="text-sm leading-6 text-muted-foreground">手机号已验证。输入该账号的两步验证密码，继续完成登录。</p>
          <Field><FieldLabel htmlFor="wa-account-password">两步验证密码</FieldLabel><Input id="wa-account-password" type="password" autoComplete="current-password" maxLength={64} value={password} disabled={busy || disabled} onChange={(event) => setPassword(event.target.value)} /></Field>
          <Button type="submit" disabled={busy || disabled || !password || passwordWait > 0}>{passwordWait > 0 ? `${passwordWait} 秒后可继续` : flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN ? '重新提交密码' : '验证并登录'}</Button>
        </form>}
        {transferChallenge && <p role="status" className="text-sm text-muted-foreground">扫码迁移暂不可用，请保留当前添加流程并检查登录状态。</p>}
        {recoveryPending && <Button disabled={busy || disabled} onClick={() => void run(() => deliverAccountAdd(flow?.flow_id || ''))}>检查登录并完成添加</Button>}
        {deliveryPending && <div className="grid gap-2"><p className="text-sm text-muted-foreground">账号验证已完成，继续完成添加即可。</p><Button disabled={busy || disabled} onClick={() => void run(() => deliverAccountAdd(flow?.flow_id || ''))}>完成添加</Button></div>}
        {flow?.last_error?.message && <p role="alert" className="text-sm text-destructive">{addingError(flow, now)}</p>}
        {flow && <div className="flex items-center justify-between text-xs text-muted-foreground"><span>{ended ? '' : `本次操作剩余约 ${remaining} 分钟`}</span><Button size="sm" variant="ghost" disabled={busy} onClick={() => void cancel()}>{ended ? '继续添加' : '结束本次添加'}</Button></div>}
      </CardContent>
    </Card>
  );
}

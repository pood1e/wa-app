import { useEffect, useState } from 'react';
import { CheckCircle2, Search } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { probeWaPhoneSMS, registerWaPhone, submitWaRegistrationOTP, type WaWorkflowResponse } from './wa-api';
import { probeMatchesValues, registrationFailureMessage, workflowText, type WaAccountAddProbeState } from './wa-account-add-model';
import { WhatsAppIcon } from './wa-brand-icon';
import { accountReasonLabel, countdownLabel } from './wa-result-labels';
import { waProbeStatus } from './wa-result-model';
import { WaRegistrationChannelButtons } from './wa-registration-channel-buttons';
import { WaRegistrationOtpCard, WA_REGISTRATION_OTP_LENGTH } from './wa-registration-otp-card';
import {
  registrationAnyMethodAvailable,
  registrationChannelsHardBlocked,
  registrationMinimumCooldownSeconds,
  type SelectableRegistrationMethodOption,
} from './wa-registration-methods';
import { resolveWaPhoneTarget, type WaResolvedPhone } from './wa-utils';
type PendingRegistration = { accountID: string; verificationRequestID: string };
type Props = { disabled?: boolean; onChanged: () => void | Promise<void>; onDone: (message: string) => void; onError: (message: string) => void };
export function WaAccountAdd({ disabled, onChanged, onDone, onError }: Props) {
  const [phone, setPhone] = useState('');
  const [countryCallingCode, setCountryCallingCode] = useState('');
  const [probe, setProbe] = useState<WaAccountAddProbeState>(null);
  const [pending, setPending] = useState<PendingRegistration | null>(null);
  const [registrationResult, setRegistrationResult] = useState<WaWorkflowResponse | null>(null);
  const [registrationTarget, setRegistrationTarget] = useState<WaResolvedPhone | null>(null);
  const [cooldownStartedAt, setCooldownStartedAt] = useState(Date.now());
  const [clockNow, setClockNow] = useState(Date.now());
  const [otp, setOtp] = useState('');
  const [busy, setBusy] = useState(false);
  const samePhone = probeMatchesValues(probe, phone, countryCallingCode);
  const currentTarget = resolveWaPhoneTarget(phone, countryCallingCode).target;
  const hasPhoneTarget = Boolean(currentTarget);
  const registrationSamePhone = Boolean(registrationTarget && currentTarget?.e164 === registrationTarget.e164);
  const activeRegistrationResult = registrationSamePhone ? registrationResult : null;
  const status = waProbeStatus(activeRegistrationResult || (samePhone ? probe?.result : null));
  const channelStatus = samePhone ? waProbeStatus(activeRegistrationResult || probe?.result) : null;
  const cooldownElapsedSeconds = Math.max(0, (clockNow - cooldownStartedAt) / 1000);
  const blocked = status.blocked === true;
  const channelsHardBlocked = registrationChannelsHardBlocked(channelStatus);
  const nextCooldownSeconds = registrationMinimumCooldownSeconds(channelStatus, cooldownElapsedSeconds);
  const canRegister = samePhone && registrationAnyMethodAvailable(channelStatus, cooldownElapsedSeconds) && !channelsHardBlocked;
  const detected = samePhone && Boolean(channelStatus);
  const badgeVariant = pending ? 'default' : blocked ? 'destructive' : canRegister ? 'default' : detected ? 'secondary' : 'outline';
  const badgeLabel = accountAddBadgeLabel(Boolean(pending), blocked, canRegister, nextCooldownSeconds, detected);

  useEffect(() => {
    const activeResult = activeRegistrationResult || (samePhone ? probe?.result : null);
    if (!activeResult) return undefined;
    const timer = window.setInterval(() => setClockNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeRegistrationResult, probe, samePhone]);

  async function runProbe() {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入手机号和国家拨号码');
    setBusy(true);
    try {
      setRegistrationResult(null);
      setRegistrationTarget(null);
      setPending(null);
      const result = await probeWaPhoneSMS(resolved.target.input);
      resetCooldownClock();
      setProbe({ target: resolved.target, result });
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function submitOTP() {
    if (!pending) return onError('没有等待中的 OTP');
    const code = otp.trim();
    if (!code) return onError('请输入 OTP');
    if (code.length !== WA_REGISTRATION_OTP_LENGTH) return onError(`请输入 ${WA_REGISTRATION_OTP_LENGTH} 位 OTP`);
    setBusy(true);
    try {
      const result = await submitWaRegistrationOTP(pending.accountID, code);
      if (result.success === false || result.error_message) throw new Error(accountReasonLabel(result.error_message, result.status) || 'OTP 提交失败');
      setOtp('');
      setPending(null);
      onDone('OTP 已提交');
      await onChanged();
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  async function startRegistration(method: SelectableRegistrationMethodOption) {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入手机号和国家拨号码');
    if (!samePhone || !channelStatus) return onError('请先检测验证通道');
    setBusy(true);
    try {
      const result = await registerWaPhone(resolved.target.input, method.value);
      const resultStatus = waProbeStatus(result);
      resetCooldownClock();
      setRegistrationResult(result);
      setRegistrationTarget(resolved.target);
      if (result.success === false || result.error_message || resultStatus.blocked === true || resultStatus.requestFailed) {
        onError(registrationFailureMessage(result, resultStatus));
        return;
      }
      const accountID = workflowText(result, 'wa_account_id');
      const verificationRequestID = workflowText(result, 'verification_request_id');
      if (accountID) setPending({ accountID, verificationRequestID });
      setProbe(null);
      setOtp('');
      onDone(accountID ? 'OTP 已发送' : '已发起');
      await onChanged();
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }
  function resetCooldownClock() {
    const now = Date.now();
    setCooldownStartedAt(now);
    setClockNow(now);
  }
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div className="grid gap-1"><CardTitle className="inline-flex items-center gap-2 text-base"><WhatsAppIcon className="size-5" />添加 WAAccount</CardTitle></div>
        <Badge variant={badgeVariant}>
          {canRegister ? <CheckCircle2 size={12} /> : null}
          {badgeLabel}
        </Badge>
      </CardHeader>
      <CardContent className="grid gap-3">
        <FieldGroup>
          <div className="grid gap-3 sm:grid-cols-[160px_1fr]">
            <Field><FieldLabel>国家拨号码</FieldLabel><Input placeholder="+1" value={countryCallingCode} onChange={(event) => setCountryCallingCode(event.target.value)} disabled={busy || disabled} /></Field>
            <Field>
              <FieldLabel>手机号</FieldLabel>
              <div className="flex gap-2">
                <Input placeholder="4155550123" value={phone} onChange={(event) => setPhone(event.target.value)} disabled={busy || disabled} />
                <Button type="button" size="icon" variant="outline" disabled={busy || disabled} title="检测手机号" aria-label="检测手机号" onClick={() => void runProbe()}><Search size={14} /></Button>
              </div>
            </Field>
          </div>
          {probe && !samePhone && <Badge variant="outline">号码已变化，请重新检测</Badge>}
        </FieldGroup>
        <Field>
          <FieldLabel>通道</FieldLabel>
          <WaRegistrationChannelButtons
            status={channelStatus}
            elapsedSeconds={cooldownElapsedSeconds}
            phoneReady={hasPhoneTarget}
            disabled={busy || disabled || Boolean(pending) || channelsHardBlocked}
            onStart={(method) => void startRegistration(method)}
          />
          <p className="px-1 text-[11px] text-muted-foreground">「旧设备」用于从真机转入账号：真机 WhatsApp 仍在线时，验证码会发送到真机，读取后填入下方完成转入。</p>
        </Field>
        {pending && <WaRegistrationOtpCard value={otp} busy={busy} onChange={setOtp} onSubmit={() => void submitOTP()} />}
      </CardContent>
    </Card>
  );
}

function accountAddBadgeLabel(pending: boolean, blocked: boolean, canRegister: boolean, cooldownSeconds: number, detected: boolean) {
  if (pending) return '等待 OTP';
  if (blocked) return '已封禁';
  if (canRegister) return '可注册';
  if (cooldownSeconds > 0) return `冷却 ${countdownLabel(cooldownSeconds)}`;
  if (detected) return '暂无可用';
  return '待检测';
}

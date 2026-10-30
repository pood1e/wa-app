import { type FormEvent, type ReactNode, useEffect, useMemo, useState } from 'react';
import { ArrowRightLeft, Mail, RefreshCw, ShieldCheck } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AccountSettingsOperationStatus } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { AccountSettingsOperation, GetTwoFactorAuthStatusResponse } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { WaAccountProjection } from './wa-api';
import { getWaTwoFactorAuthStatus, requestWaAccountEmailOtp, setWaAccountEmail, setWaTwoFactorAuthSettings, verifyWaAccountEmailOtp, waAccountID, waKeys } from './wa-api';
import { WaAccountChangeNumberCard } from './wa-account-change-number-card';
import { WaEmailOtpForm } from './wa-email-otp-form';
import { WaAccountPasswordForm } from './wa-account-password-form';
import {
  emailBadgeVariant,
  emailStatusLabel,
  initialTwoFactorStatus,
  shouldCollectEmailOtpAfterSet,
  shouldShowEmailOtp,
  statusLabel,
  twoFactorBadgeVariant,
  twoFactorConfigured,
  twoFactorEmailConfigured,
  twoFactorStatusLabel,
} from './wa-account-security-model';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';

type Props = { account: WaAccountProjection; onDone: (message: string) => void; onError: (message: string) => void };
type TwoFactorStatusData = NonNullable<GetTwoFactorAuthStatusResponse['status']>;

export function WaAccountSecurityPanel({ account, onDone, onError }: Props) {
  const queryClient = useQueryClient();
  const [password, setPassword] = useState('');
  const [email, setEmail] = useState('');
  const [emailOtp, setEmailOtp] = useState('');
  const [emailOtpVisible, setEmailOtpVisible] = useState(false);
  const [emailOtpOperation, setEmailOtpOperation] = useState<AccountSettingsOperation>();
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [emailOpen, setEmailOpen] = useState(false);
  const [changeNumberOpen, setChangeNumberOpen] = useState(false);
  const [lastStatus, setLastStatus] = useState<AccountSettingsOperationStatus | undefined>();
  const handleError = (error: unknown) => onError(error instanceof Error ? error.message : String(error));
  const handleSuccess = (message: string, status?: AccountSettingsOperationStatus) => { setLastStatus(status); onDone(message); };
  const accountID = waAccountID(account);
  const statusKey = useMemo(() => waKeys.twoFactorStatus(accountID), [accountID]);
  const patchStatus = (patch: Partial<TwoFactorStatusData>) =>
    queryClient.setQueryData<GetTwoFactorAuthStatusResponse>(statusKey, (previous) => ({
      error: previous?.error,
      status: {
        configured: previous?.status?.configured || false,
        email_configured: previous?.status?.email_configured || false,
        email_verified: previous?.status?.email_verified || false,
        email_confirmed: previous?.status?.email_confirmed || false,
        email_address: previous?.status?.email_address || '',
        ...patch,
      },
    }));
  const twoFactorStatus = useQuery({
    queryKey: statusKey,
    queryFn: ({ signal }) => getWaTwoFactorAuthStatus(account, { remoteRefresh: true, signal }),
    enabled: false,
    gcTime: 30 * 60_000,
    initialData: () => initialTwoFactorStatus(account.two_factor_auth),
    staleTime: Infinity,
  });
  const passwordConfigured = twoFactorConfigured(twoFactorStatus);
  const emailConfigured = twoFactorEmailConfigured(twoFactorStatus);
  const passwordAction = passwordConfigured ? '修改验证密码' : '设置验证密码';
  const emailAction = emailConfigured ? '修改账户邮箱' : '设置账户邮箱';
  const twoFactor = useMutation({
    mutationFn: () => setWaTwoFactorAuthSettings(account, password),
    onSuccess: (resp) => {
      if (resp.operation?.status !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED) { handleError(new Error('未确认两步验证已开启，请同步状态后重试')); return; }
      setPassword('');
      setPasswordOpen(false);
      patchStatus({ configured: true });
      handleSuccess(passwordConfigured ? '两步验证密码已修改' : '两步验证密码已设置', resp.operation?.status);
    },
    onError: handleError,
  });
  const otpRequest = useMutation({
    mutationFn: () => requestWaAccountEmailOtp(account),
    onSuccess: (resp) => {
      setEmailOtpVisible(true);
      setEmailOtpOperation(resp.operation);
      handleSuccess('请查收邮箱验证码', resp.operation?.status);
    },
    onError: handleError,
  });
  const emailSet = useMutation({
    mutationFn: () => setWaAccountEmail(account, { email_address: email }),
    onSuccess: (resp) => {
      const setStatus = resp.operation?.status;
      const verified = setStatus === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED;
      const needsVerification = shouldCollectEmailOtpAfterSet(setStatus);
      setEmailOtpVisible(needsVerification);
      setEmailOtpOperation(undefined);
      if (setStatus !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED) {
        patchStatus({ email_address: email, email_configured: true, email_verified: verified, email_confirmed: false });
      }
      if (verified) { setEmailOtp(''); setEmailOpen(false); }
      if (needsVerification) otpRequest.mutate();
      else handleSuccess('账户邮箱已验证', setStatus);
    },
    onError: handleError,
  });
  const otpVerify = useMutation({
    mutationFn: () => verifyWaAccountEmailOtp(account, emailOtp),
    onSuccess: (resp) => {
      const status = resp.operation?.status;
      setEmailOtp('');
      setEmailOtpOperation(resp.operation);
      setEmailOtpVisible(shouldShowEmailOtp(status));
      if (status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED) { patchStatus({ email_configured: true, email_verified: true }); setEmailOpen(false); }
      if (status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH) {
        setLastStatus(status);
        onError('邮箱验证码不匹配');
      } else handleSuccess('邮箱验证成功', status);
    },
    onError: handleError,
  });
  const busy = twoFactor.isPending || emailSet.isPending || otpRequest.isPending || otpVerify.isPending;
  const syncing = twoFactorStatus.isFetching;
  const syncFailed = twoFactorStatus.isError && !syncing;
  const refreshStatus = async () => {
    const result = await twoFactorStatus.refetch();
    if (result.isError) onError(syncFailureMessage(result.error));
  };
  useEffect(() => {
    if (account.two_factor_auth) queryClient.setQueryData(statusKey, initialTwoFactorStatus(account.two_factor_auth));
  }, [account.two_factor_auth, queryClient, statusKey]);
  useEffect(() => {
    setPassword('');
    setEmail('');
    setEmailOtp('');
    setEmailOtpVisible(false);
    setEmailOtpOperation(undefined);
    setPasswordOpen(false);
    setEmailOpen(false);
    setChangeNumberOpen(false);
  }, [accountID]);
  return (
    <section className="grid gap-4">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0 text-xs text-muted-foreground">{syncFailed ? '远程同步失败,以下为最近缓存状态(账号需在线才能同步)。' : '点击右侧同步从 WhatsApp 拉取最新状态。'}</div>
        <div className="flex items-center gap-2">
          {lastStatus !== undefined ? <Badge variant="outline">{statusLabel(lastStatus)}</Badge> : null}
          <Button size="icon" variant="ghost" type="button" disabled={busy || syncing} title="同步状态" aria-label="同步状态" onClick={() => void refreshStatus()}><RefreshCw size={16} className={syncing ? 'animate-spin' : ''} /></Button>
        </div>
      </div>
      <div className="divide-y divide-border">
        <SettingRow
          icon={<ShieldCheck size={15} />}
          title="两步验证密码"
          badge={<Badge variant={twoFactorBadgeVariant(twoFactorStatus)}>{twoFactorStatusLabel(twoFactorStatus)}</Badge>}
          actionLabel={passwordAction}
          disabled={busy}
          onAction={() => { setPassword(''); setPasswordOpen(true); }}
        />
        <SettingRow
          icon={<Mail size={15} />}
          title="账户邮箱"
          badge={<Badge variant={emailBadgeVariant(twoFactorStatus)}>{emailStatusLabel(twoFactorStatus)}</Badge>}
          detail={twoFactorStatus.data?.status?.email_address}
          actionLabel={emailAction}
          disabled={busy}
          onAction={() => { setEmail(''); setEmailOtp(''); setEmailOtpVisible(false); setEmailOpen(true); }}
        />
        <SettingRow
          icon={<ArrowRightLeft size={15} />}
          title="换绑手机号"
          detail={account.phone?.e164_number}
          actionLabel="换绑手机号"
          disabled={busy}
          onAction={() => setChangeNumberOpen(true)}
        />
      </div>

      <Dialog open={passwordOpen} onOpenChange={(open) => { setPasswordOpen(open); if (!open) setPassword(''); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle><ShieldCheck size={16} />{passwordAction}</DialogTitle>
            <DialogDescription>{passwordConfigured ? '输入新的两步验证密码。' : '设置两步验证密码，保护账号登录。'}</DialogDescription>
          </DialogHeader>
          <WaAccountPasswordForm password={password} busy={busy} configured={passwordConfigured} onPasswordChange={setPassword} onSubmit={(event) => submit(event, twoFactor.mutate)} />
        </DialogContent>
      </Dialog>

      <Dialog open={emailOpen} onOpenChange={(open) => { setEmailOpen(open); if (!open) { setEmail(''); setEmailOtp(''); setEmailOtpVisible(false); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle><Mail size={16} />{emailAction}</DialogTitle>
            <DialogDescription>{emailConfigured ? '更换账户邮箱,可能需要邮箱验证码确认。' : '绑定账户邮箱用于找回与验证。'}</DialogDescription>
          </DialogHeader>
          <EmailForm email={email} busy={busy} configured={emailConfigured} onEmailChange={(value) => { setEmail(value); setEmailOtp(''); setEmailOtpVisible(false); }} onSubmit={(event) => submit(event, emailSet.mutate)} />
          {emailOtpVisible && (
            <WaEmailOtpForm operation={emailOtpOperation} code={emailOtp} busy={busy} onCodeChange={setEmailOtp} onRequest={() => otpRequest.mutate()} onVerify={() => otpVerify.mutate()} />
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={changeNumberOpen} onOpenChange={setChangeNumberOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle><ArrowRightLeft size={16} />换绑手机号</DialogTitle>
            <DialogDescription>把当前账号迁移到新的手机号(已登录账号的 Change number)。</DialogDescription>
          </DialogHeader>
          <WaAccountChangeNumberCard account={account} busy={busy} onError={onError} />
        </DialogContent>
      </Dialog>
    </section>
  );
}

function SettingRow({ icon, title, badge, detail, actionLabel, disabled, onAction }: { icon: ReactNode; title: string; badge?: ReactNode; detail?: string; actionLabel: string; disabled?: boolean; onAction: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3 py-4 first:pt-1 last:pb-1">
      <div className="flex min-w-0 items-center gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-full bg-accent text-accent-foreground">{icon}</span>
        <div className="min-w-0"><div className="text-sm font-medium">{title}</div><div className="mt-1 flex flex-wrap items-center gap-2">{badge}{detail ? <span className="truncate text-xs text-muted-foreground">{detail}</span> : null}</div></div>
      </div>
      <Button className="rounded-full" type="button" variant="outline" size="sm" disabled={disabled} onClick={onAction}>{actionLabel}</Button>
    </div>
  );
}

function EmailForm({ email, busy, configured, onEmailChange, onSubmit }: { email: string; busy: boolean; configured: boolean; onEmailChange: (value: string) => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  return (
    <form className="grid gap-3" onSubmit={onSubmit}>
      <FieldGroup>
        <Field>
          <FieldLabel>{configured ? '新邮箱地址' : '邮箱地址'}</FieldLabel>
          <Input value={email} onChange={(event) => onEmailChange(event.target.value)} type="email" disabled={busy} placeholder={configured ? '新邮箱地址' : '邮箱地址'} autoFocus />
        </Field>
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={busy || !email}><Mail size={14} />{configured ? '修改邮箱' : '设置邮箱'}</Button>
    </form>
  );
}

function syncFailureMessage(error: unknown) {
  const raw = error instanceof Error ? error.message : '';
  return raw ? `账号安全同步失败:${raw}(账号需在线才能同步)` : '账号安全同步失败:账号可能未在线';
}

function submit(event: FormEvent<HTMLFormElement>, run: () => void) { event.preventDefault(); run(); }

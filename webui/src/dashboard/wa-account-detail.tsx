import { type ReactNode, useState } from 'react';
import { Fingerprint, KeyRound, Shield, UserRound } from 'lucide-react';
import { WAAccountStatus } from '../proto/byte/v/forge/waapp/v1/profile';
import type { ClientProfile, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { submitWaRegistrationOTP, waAccountID } from './wa-api';
import { WaAccountProfileSettings } from './wa-account-profile-settings';
import { WaAccountSecurityPanel } from './wa-account-security';
import { WaDeviceFingerprintPanel } from './wa-device-fingerprint';
import { WA_REGISTRATION_OTP_LENGTH } from './wa-registration-otp-card';
import { accountReasonLabel, type StatusView } from './wa-result-labels';
import { useWaLongConnectionIndex, waAccountDisplayStatus } from './wa-long-connection-badge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table';

type Props = {
  account: WAAccount;
  profiles: ClientProfile[];
  profilesLoading: boolean;
  busy: boolean;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
  onAvatarChanged: () => void;
};

export function WaAccountDetail({ account, profiles, profilesLoading, busy, onDone, onError, onAccountChanged, onAvatarChanged }: Props) {
  return (
    <section className="grid gap-4">
      {isRegistrationPending(account) && <ManualOtpSubmit account={account} busy={busy} onDone={onDone} onError={onError} />}
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(24rem,0.95fr)]">
        <InfoPanel icon={<UserRound size={16} />} title="资料">
          <div className="grid gap-4">
            <WaAccountProfileSettings account={account} onDone={onDone} onError={onError} onAccountChanged={onAccountChanged} onAvatarChanged={onAvatarChanged} />
            <InfoGrid account={account} />
          </div>
        </InfoPanel>
        <InfoPanel icon={<Shield size={16} />} title="安全">
          <WaAccountSecurityPanel account={account} onDone={onDone} onError={onError} />
        </InfoPanel>
      </div>
      <InfoPanel icon={<Fingerprint size={16} />} title="设备指纹">
        <WaDeviceFingerprintPanel profiles={profiles} loading={profilesLoading} />
      </InfoPanel>
    </section>
  );
}

function InfoPanel({ title, icon, children }: { title: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="inline-flex items-center gap-2 text-sm">{icon}{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}

function isRegistrationPending(account: WAAccount) {
  return account.status === WAAccountStatus.WA_ACCOUNT_STATUS_PENDING_REGISTRATION;
}

function ManualOtpSubmit({ account, busy, onDone, onError }: { account: WAAccount; busy: boolean; onDone: (message: string) => void; onError: (message: string) => void }) {
  const [otp, setOtp] = useState('');
  async function submit() {
    const code = otp.trim();
    if (code.length !== WA_REGISTRATION_OTP_LENGTH) return onError(`请输入 ${WA_REGISTRATION_OTP_LENGTH} 位 OTP`);
    try {
      const resp = await submitWaRegistrationOTP(account, code);
      if (resp.error_message || resp.success === false) throw new Error(accountReasonLabel(resp.error_message, resp.status) || 'OTP 提交失败');
      setOtp('');
      onDone('OTP 已提交');
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    }
  }
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="inline-flex items-center gap-2 text-sm"><KeyRound size={15} />提交注册 OTP</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex gap-2">
          <Input value={otp} onChange={(event) => setOtp(event.target.value.replace(/\D/g, '').slice(0, WA_REGISTRATION_OTP_LENGTH))} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={WA_REGISTRATION_OTP_LENGTH} placeholder={`${WA_REGISTRATION_OTP_LENGTH} 位验证码`} />
          <Button disabled={busy || otp.trim().length !== WA_REGISTRATION_OTP_LENGTH} onClick={() => void submit()}>提交</Button>
        </div>
      </CardContent>
    </Card>
  );
}

function InfoGrid({ account }: { account: WAAccount }) {
  const { byAccount } = useWaLongConnectionIndex();
  const connection = byAccount.get(account.wa_account_id);
  const rows: Array<{ label: string; value: ReactNode }> = [
    { label: '名称', value: account.display_name?.trim() || '-' },
    { label: '账号 ID', value: waAccountID(account) },
    { label: '状态', value: <StatusBadge view={waAccountDisplayStatus(account, connection)} /> },
    { label: '手机号', value: account.phone?.e164_number || '-' },
    { label: '国家', value: account.phone?.country_iso2 || '-' },
    { label: '拨号码', value: account.phone?.country_calling_code || '-' },
    { label: '创建时间', value: formatTime(account.audit?.created_at) },
    { label: '更新时间', value: formatTime(account.audit?.updated_at) },
  ];
  return <Table><TableBody>{rows.map((row) => <InfoRow key={row.label} label={row.label} value={row.value} />)}</TableBody></Table>;
}

function InfoRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell className="w-24 text-muted-foreground">{label}</TableCell>
      <TableCell className="max-w-0 truncate">{typeof value === 'string' ? <span className="font-mono text-xs">{value}</span> : value}</TableCell>
    </TableRow>
  );
}

function StatusBadge({ view }: { view: StatusView }) {
  return <Badge variant={view.variant} className="gap-1.5"><span className={`size-1.5 rounded-full ${statusDotClass(view.tone)}`} />{view.label}</Badge>;
}

function statusDotClass(tone: StatusView['tone']) {
  if (tone === 'ok') return 'bg-emerald-500';
  if (tone === 'warn') return 'bg-amber-500';
  if (tone === 'bad') return 'bg-destructive';
  return 'bg-muted-foreground/50';
}

function formatTime(value?: string) {
  if (!value) return '-';
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}

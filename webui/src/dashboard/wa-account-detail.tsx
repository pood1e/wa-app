import type { ReactNode } from 'react';
import { Globe, Shield, UserRound } from 'lucide-react';
import { WAAccountStatus, type WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { WaAccountProfileSettings } from './wa-account-profile-settings';
import { WaAccountSecurityPanel } from './wa-account-security';
import { WaAccountReview } from './wa-account-review';
import { WaAccountProxySettings } from './wa-account-proxy';
import { LongConnectionStatus } from '../proto/byte/v/forge/waapp/v1/messaging';
import { type StatusView } from './wa-result-labels';
import { useWaLongConnectionIndex, waAccountDisplayStatus } from './wa-long-connection-badge';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table';

type Props = {
  account: WAAccount;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
  onAvatarChanged: () => void;
};

export function WaAccountDetail({ account, onDone, onError, onAccountChanged, onAvatarChanged }: Props) {
  const readOnly = account.status !== WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE;
  const { byAccount } = useWaLongConnectionIndex();
  const connection = byAccount.get(account.wa_account_id);
  const online = connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING;
  return (
    <section className="grid items-start gap-5 lg:grid-cols-[320px_minmax(0,1fr)]">
      <InfoPanel icon={<UserRound size={16} />} title="个人资料">
        <div className="grid gap-4">
          <fieldset disabled={readOnly}><WaAccountProfileSettings account={account} onDone={onDone} onError={onError} onAccountChanged={onAccountChanged} onAvatarChanged={onAvatarChanged} /></fieldset>
          <InfoGrid account={account} />
        </div>
      </InfoPanel>
      <div className="grid gap-5">
        <InfoPanel icon={<Shield size={16} />} title="账号安全">
          <fieldset disabled={readOnly}><WaAccountSecurityPanel account={account} onDone={onDone} onError={onError} /></fieldset>
        </InfoPanel>
        {!readOnly && !online ? <WaAccountReview key={account.wa_account_id} accountID={account.wa_account_id} /> : null}
        <InfoPanel icon={<Globe size={16} />} title="网络设置">
          <WaAccountProxySettings accountID={account.wa_account_id} onDone={onDone} onError={onError} />
        </InfoPanel>
      </div>
    </section>
  );
}

function InfoPanel({ title, icon, children }: { title: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <Card className="gap-5 rounded-xl py-5 ring-border" size="sm">
      <CardHeader className="px-5">
        <CardTitle className="inline-flex items-center gap-2 text-sm text-primary">{icon}{title}</CardTitle>
      </CardHeader>
      <CardContent className="px-5">{children}</CardContent>
    </Card>
  );
}

function InfoGrid({ account }: { account: WAAccount }) {
  const { byAccount } = useWaLongConnectionIndex();
  const connection = byAccount.get(account.wa_account_id);
  const rows: Array<{ label: string; value: ReactNode }> = [
    { label: '手机号', value: account.phone?.e164_number || '-' },
    { label: '地区', value: account.phone?.country_iso2 || '-' },
    { label: '连接状态', value: <StatusBadge view={waAccountDisplayStatus(account, connection)} /> },
    { label: '添加时间', value: formatTime(account.audit?.created_at) },
  ];
  return <Table><TableBody>{rows.map((row) => <InfoRow key={row.label} label={row.label} value={row.value} />)}</TableBody></Table>;
}

function InfoRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell className="w-20 px-0 py-3 text-xs text-muted-foreground">{label}</TableCell>
      <TableCell className="max-w-0 truncate py-3 pr-0 text-right">{typeof value === 'string' ? <span className="text-xs" title={value}>{value}</span> : value}</TableCell>
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
  return Number.isNaN(time.getTime()) ? value : time.toLocaleDateString('zh-CN');
}

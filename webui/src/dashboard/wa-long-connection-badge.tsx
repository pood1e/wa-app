import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import { WAAccountStatus, type WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { getWaConnections, waKeys } from './wa-api';
import { waAccountStatusView, type StatusView } from './wa-result-labels';

type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

// 账号被转出/在其他设备登录后,长连接被作废:终态 STOPPED 且 last_error 为 CONFLICT。
export function isConnectionTransferredOut(connection?: LongConnectionState) {
  return connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED && connection?.last_error?.code === WaErrorCode.WA_ERROR_CODE_CONFLICT;
}

// waAccountDisplayStatus 把账号对外状态派生自「账号生命周期 + 长连接实况」,避免 account.status
// 与连接实况漂移(连接已停/失效却仍显示“正常”)。非 ACTIVE 生命周期(等待验证码/暂停/归档/已转出)
// 按其本身展示;ACTIVE 账号则反映连接实况:已连接→正常,启动/重连→连接中,STOPPED+CONFLICT→已转出
// (被接管),其余(已停止/失败/无连接)→离线。
export function waAccountDisplayStatus(account: WAAccount, connection?: LongConnectionState): StatusView {
  if (account.status !== WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE) return waAccountStatusView(account.status);
  if (isConnectionTransferredOut(connection)) return { label: '已转出', variant: 'destructive', tone: 'bad' };
  const status = connection?.status;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING) return { label: '正常', variant: 'default', tone: 'ok' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING || status === LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING) return { label: '连接中', variant: 'secondary', tone: 'warn' };
  return { label: '离线', variant: 'outline', tone: 'idle' };
}

export function useWaLongConnectionIndex() {
  const query = useQuery({ queryKey: waKeys.connections(), queryFn: () => getWaConnections(), refetchInterval: 5000 });
  const byAccount = useMemo(() => indexConnections(query.data?.connections || []), [query.data?.connections]);
  return { byAccount, loading: query.isLoading };
}

export function WaLongConnectionBadge({ connection, loading }: { connection?: LongConnectionState; loading?: boolean }) {
  const view = statusView(connection?.status, loading, isConnectionTransferredOut(connection));
  return <Badge variant={view.variant}>长连接：{view.label}</Badge>;
}

function indexConnections(connections: LongConnectionState[]) {
  return connections.reduce((acc, connection) => {
    if (!connection.wa_account_id) return acc;
    acc.set(connection.wa_account_id, betterConnection(acc.get(connection.wa_account_id), connection));
    return acc;
  }, new Map<string, LongConnectionState>());
}

function betterConnection(current: LongConnectionState | undefined, next: LongConnectionState) {
  if (!current) return next;
  return statusRank(next.status) < statusRank(current.status) ? next : current;
}

function statusView(status: LongConnectionStatus | undefined, loading?: boolean, transferredOut?: boolean): { label: string; variant: BadgeVariant } {
  if (loading && !status) return { label: '加载中', variant: 'secondary' };
  if (transferredOut) return { label: '已转出', variant: 'destructive' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING) return { label: '已连接', variant: 'default' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING) return { label: '重连中', variant: 'secondary' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING) return { label: '启动中', variant: 'secondary' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_FAILED) return { label: '失败', variant: 'destructive' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED) return { label: '已停止', variant: 'outline' };
  return { label: '未启动', variant: 'outline' };
}

function statusRank(status: LongConnectionStatus | undefined) {
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING) return 0;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING) return 1;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING) return 2;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_FAILED) return 3;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED) return 4;
  return 5;
}

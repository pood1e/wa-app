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

export function isConnectionRejected(connection?: LongConnectionState) {
  return connection?.last_error?.code === WaErrorCode.WA_ERROR_CODE_AUTHENTICATION_FAILED;
}

export function waAccountDisplayStatus(account: WAAccount, connection?: LongConnectionState): StatusView {
  if (account.status !== WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE) return waAccountStatusView(account.status);
  if (isConnectionTransferredOut(connection)) return { label: '已转出', variant: 'destructive', tone: 'bad' };
  if (isConnectionRejected(connection)) return { label: '登录已失效', variant: 'destructive', tone: 'bad' };
  const status = connection?.status;
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING) return { label: '正常', variant: 'default', tone: 'ok' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING || status === LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING) return { label: '连接中', variant: 'secondary', tone: 'warn' };
  return { label: '离线', variant: 'outline', tone: 'idle' };
}

export function useWaLongConnectionIndex() {
  const query = useQuery({ queryKey: waKeys.connections(), queryFn: ({ signal }) => getWaConnections({}, signal), refetchInterval: 5000 });
  const byAccount = useMemo(() => indexConnections(query.data?.connections || []), [query.data?.connections]);
  return { byAccount, loading: query.isLoading };
}

export function WaLongConnectionBadge({ connection, loading }: { connection?: LongConnectionState; loading?: boolean }) {
  const view = isConnectionRejected(connection) ? { label: '登录已失效', variant: 'destructive' as const } : statusView(connection?.status, loading, isConnectionTransferredOut(connection));
  return <Badge variant={view.variant}>长连接：{view.label}</Badge>;
}

export function indexConnections(connections: LongConnectionState[]) {
  return connections.reduce((acc, connection) => {
    if (!connection.wa_account_id) return acc;
    acc.set(connection.wa_account_id, betterConnection(acc.get(connection.wa_account_id), connection));
    return acc;
  }, new Map<string, LongConnectionState>());
}

function betterConnection(current: LongConnectionState | undefined, next: LongConnectionState) {
  if (!current) return next;
  const nextRank = statusRank(next.status);
  const currentRank = statusRank(current.status);
  if (nextRank !== currentRank) return nextRank < currentRank ? next : current;
  const nextTime = connectionTime(next);
  const currentTime = connectionTime(current);
  if (nextTime !== currentTime) return nextTime > currentTime ? next : current;
  return next.last_error && !current.last_error ? next : current;
}

function connectionTime(connection: LongConnectionState) {
  return Math.max(...[connection.started_at, connection.last_connected_at, connection.last_heartbeat_at].map((value) => Date.parse(value || '') || 0));
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

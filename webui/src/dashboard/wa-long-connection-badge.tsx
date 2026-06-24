import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import { getWaConnections, waKeys } from './wa-api';

type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

// 账号被转出/在其他设备登录后,长连接被作废:终态 STOPPED 且 last_error 为 CONFLICT。
export function isConnectionTransferredOut(connection?: LongConnectionState) {
  return connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED && connection?.last_error?.code === WaErrorCode.WA_ERROR_CODE_CONFLICT;
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

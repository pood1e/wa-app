import { AccountSettingsOperationStatus } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { GetTwoFactorAuthStatusResponse, TwoFactorAuthStatus } from '../proto/byte/v/forge/waapp/v1/account_settings';

type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

export type TwoFactorStatusView = { isFetching: boolean; isError: boolean; data?: { status?: TwoFactorAuthStatus } };

export function initialTwoFactorStatus(status?: TwoFactorAuthStatus): GetTwoFactorAuthStatusResponse {
  return status ? { status, error: undefined } : { status: undefined, error: undefined };
}

export function shouldCollectEmailOtpAfterSet(status?: AccountSettingsOperationStatus) {
  return status !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED
    && status !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED;
}

export function shouldShowEmailOtp(status?: AccountSettingsOperationStatus) {
  return status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION
    || status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING
    || status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH;
}

// 远程同步失败时,优先展示已缓存的最近状态,而不是把它盖成刺眼的「同步失败」。
// 同步失败本身由面板上的软提示(toast/角标)单独表达,避免误导。
export function twoFactorStatusLabel(query: TwoFactorStatusView) {
  if (query.isFetching) return '同步中';
  if (query.data?.status) return query.data.status.configured ? '已配置' : '未配置';
  if (query.isError) return '同步失败';
  return '未同步';
}

export function emailStatusLabel(query: TwoFactorStatusView) {
  if (query.isFetching) return '同步中';
  if (query.data?.status) {
    if (query.data.status.email_verified) return '已验证';
    if (query.data.status.email_address) return '待验证';
    return query.data.status.email_configured ? '已配置' : '未配置';
  }
  if (query.isError) return '同步失败';
  return '未同步';
}

export function twoFactorBadgeVariant(query: TwoFactorStatusView): BadgeVariant {
  if (query.data?.status) return query.data.status.configured ? 'default' : 'outline';
  return query.isError ? 'destructive' : 'outline';
}

export function emailBadgeVariant(query: TwoFactorStatusView): BadgeVariant {
  if (query.data?.status) {
    if (query.data.status.email_verified) return 'default';
    return query.data.status.email_address || query.data.status.email_configured ? 'secondary' : 'outline';
  }
  return query.isError ? 'destructive' : 'outline';
}

export function twoFactorConfigured(query: TwoFactorStatusView) {
  return Boolean(query.data?.status?.configured);
}

export function twoFactorEmailConfigured(query: TwoFactorStatusView) {
  return Boolean(query.data?.status?.email_configured || query.data?.status?.email_address);
}

export function statusLabel(status?: AccountSettingsOperationStatus) {
  switch (status) {
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION: return '待邮箱验证';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING: return '等待 OTP';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED: return '已验证';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH: return '验证码不匹配';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED: return '已拒绝';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED: return '已受理';
    default: return '未执行';
  }
}

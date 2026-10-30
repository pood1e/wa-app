import { AccountAddStatus, type AccountAddFlow } from '../proto/byte/v/forge/waapp/v1/account_add';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { VerificationDeliveryMethod, VerificationRequestStatus } from '../proto/byte/v/forge/waapp/v1/registration';
import { remainingSeconds } from './wa-time';

export function isTransferMethod(method?: VerificationDeliveryMethod) {
  return method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_WA_OLD || method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER;
}

export function canSubmitCode(flow: AccountAddFlow | null) {
  return !flow?.password_challenge && (flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_CODE || flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN) &&
    (flow.verification?.status === VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_SENT ||
      flow.verification?.status === VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_WAITING);
}

export function canSubmitPassword(flow: AccountAddFlow | null) {
  return Boolean(flow?.password_challenge) &&
    (flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_PASSWORD || flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN);
}

export function canRecoverRegistration(flow: AccountAddFlow | null) {
  return (canSubmitCode(flow) || canSubmitPassword(flow)) && (flow?.status === AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN || flow?.verification?.delivery_method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER);
}

export function channelState(flow: AccountAddFlow | null, method: VerificationDeliveryMethod, now: number) {
  if (method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER) return { available: false, remaining: 0 };
  const latest = flow?.verification;
  const statuses = latest?.method_statuses?.length ? latest.method_statuses : flow?.probe?.method_statuses;
  const observed = latest?.method_statuses?.length ? latest.requested_at : flow?.probe?.probed_at;
  const status = statuses?.find((value) => value.delivery_method === method);
  const wait = Number.parseFloat(status?.cooldown || '0');
  const remaining = Math.max(remainingSeconds(status?.cooldown, observed, now), remainingSeconds(latest?.retry_after, latest?.requested_at, now));
  const eligible = status ? status.available || wait > 0 : Boolean(flow?.probe?.supported_methods?.includes(method));
  const available = eligible && remaining === 0;
  return { available, remaining };
}

export function addingError(flow: AccountAddFlow | null, now: number) {
  const error = flow?.last_error;
  if (error?.code === WaErrorCode.WA_ERROR_CODE_RATE_LIMITED && flow?.password_challenge) return '两步验证暂时冷却，请稍后重新提交密码。';
  if (error?.code === WaErrorCode.WA_ERROR_CODE_VERIFICATION_REQUIRED) {
    const remaining = remainingSeconds(flow?.verification?.retry_after, flow?.verification?.requested_at, now);
    return remaining > 0 ? '本次验证码校验未通过，请等待冷却结束后重新获取验证码。' : '本次验证码校验未通过，请重新获取验证码，并使用本次收到的新验证码。';
  }
  if (error && canRecoverRegistration(flow)) return '注册响应中断，结果尚未确认。请先检查登录状态，保留当前添加流程。';
  if (error?.code !== WaErrorCode.WA_ERROR_CODE_RATE_LIMITED) return error?.message || '';
  const remaining = remainingSeconds(flow?.verification?.retry_after, flow?.verification?.requested_at, now);
  return remaining > 0 ? '暂时无法发送验证码，请等待冷却结束后重新提交。' : '上次发码暂不可用，请重新提交或选择其他通道。';
}

export function addingStatus(flow: AccountAddFlow | null) {
  switch (flow?.status) {
    case AccountAddStatus.ACCOUNT_ADD_STATUS_READY: return '选择验证通道';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_CODE: return flow.verification?.delivery_method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER ? '等待旧设备确认' : '等待验证码';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_PASSWORD: return '输入两步验证密码';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_READY_TO_DELIVER: return '等待完成添加';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_ACCEPTED: return '已添加';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_CANCELLED: return '已结束';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_EXPIRED: return '已过期';
    case AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN: return '请求结果待确认';
    default: return '待检测';
  }
}

import { ClientProfileStatus, WAAccountStatus } from '../proto/byte/v/forge/waapp/v1/profile';

export type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';
export type StatusTone = 'ok' | 'warn' | 'bad' | 'idle';
export type StatusView = { label: string; variant: BadgeVariant; tone: StatusTone };

export function waAccountStatusView(status?: WAAccountStatus): StatusView {
  switch (status) {
    case WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE: return { label: '正常', variant: 'default', tone: 'ok' };
    case WAAccountStatus.WA_ACCOUNT_STATUS_PENDING_REGISTRATION: return { label: '等待验证码', variant: 'secondary', tone: 'warn' };
    case WAAccountStatus.WA_ACCOUNT_STATUS_PAUSED: return { label: '已暂停', variant: 'outline', tone: 'idle' };
    case WAAccountStatus.WA_ACCOUNT_STATUS_ARCHIVED: return { label: '已归档', variant: 'outline', tone: 'idle' };
    case WAAccountStatus.WA_ACCOUNT_STATUS_TRANSFERRED_OUT: return { label: '已转出', variant: 'destructive', tone: 'bad' };
    default: return { label: '状态未知', variant: 'outline', tone: 'idle' };
  }
}

export function clientProfileStatusView(status?: ClientProfileStatus): StatusView {
  switch (status) {
    case ClientProfileStatus.CLIENT_PROFILE_STATUS_READY: return { label: '可用', variant: 'default', tone: 'ok' };
    case ClientProfileStatus.CLIENT_PROFILE_STATUS_PREPARING: return { label: '准备中', variant: 'secondary', tone: 'warn' };
    case ClientProfileStatus.CLIENT_PROFILE_STATUS_REJECTED: return { label: '已拒绝', variant: 'destructive', tone: 'bad' };
    case ClientProfileStatus.CLIENT_PROFILE_STATUS_RETIRED: return { label: '已停用', variant: 'outline', tone: 'idle' };
    default: return { label: '状态未知', variant: 'outline', tone: 'idle' };
  }
}

export function oldDeviceLabel(value?: boolean, flow?: string) {
  if (value === true || flow === 'registered') return '可用';
  if (flow === 'blocked') return '不可用';
  return '未知';
}

export function booleanLabel(value?: boolean) {
  if (value === true) return '是';
  if (value === false) return '否';
  return '未知';
}

export function smsLabel(available?: boolean, waitSeconds?: number | null) {
  if (waitSeconds && waitSeconds > 0) return `冷却 ${formatSeconds(waitSeconds)}`;
  if (available === true) return '可发';
  if (available === false) return '不可发';
  return '未知';
}

export function cooldownLabel(value?: number | null) {
  return value && value > 0 ? `冷却 ${formatSeconds(value)}` : '';
}

export function countdownLabel(value?: number | null) {
  if (!value || value <= 0) return '';
  const total = Math.ceil(value);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const paddedMinutes = hours > 0 ? String(minutes).padStart(2, '0') : String(minutes);
  const paddedSeconds = String(seconds).padStart(2, '0');
  return hours > 0 ? `${hours}:${paddedMinutes}:${paddedSeconds}` : `${paddedMinutes}:${paddedSeconds}`;
}

export function methodStateLabel(available?: boolean, cooldownSeconds?: number | null) {
  const cooldown = cooldownLabel(cooldownSeconds);
  if (cooldown) return cooldown;
  if (available === true) return '可用';
  if (available === false) return '不可用';
  return '未知';
}

export function methodLabels(value: unknown) {
  const raw = Array.isArray(value) ? value.map(textOf) : textOf(value).split(',');
  const seen = new Set<string>();
  return raw.map(methodLabel).filter((label) => label && !seen.has(label) && seen.add(label));
}

function formatSeconds(value: number) {
  if (value < 60) return `${Math.ceil(value)}s`;
  const minutes = Math.ceil(value / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.ceil(minutes / 60)}h`;
}

export function methodLabel(value: string) {
  const normalized = value.trim().toUpperCase().replace(/^VERIFICATION_DELIVERY_METHOD_/, '').replace(/^REGISTRATION_LOGIN_METHOD_/, '');
  if (!normalized || normalized === 'UNSPECIFIED') return '';
  if (normalized === 'AUTOCONF') return '自动确认';
  if (normalized === 'DEEPLINK_OTP') return 'Deep Link OTP';
  if (normalized === 'SEND_SMS' || normalized === 'SEND_SMS_TO_WA') return '发送 SMS 至 WA';
  if (normalized === 'SMS') return 'SMS';
  if (normalized === 'VOICE') return '语音';
  if (normalized === 'IN_APP_MESSAGE' || normalized === 'WA_OLD' || normalized === 'OLD_WA') return '旧设备';
  if (normalized === 'PASSKEY') return 'Passkey';
  if (normalized === 'DISCOVERABLE_CREDENTIAL') return '可发现凭据';
  if (normalized === 'SILENT_AUTH') return '静默验证';
  if (normalized === 'SILENT_AUTH_TS43' || normalized === 'SILENT_AUTH_TS_43') return '静默验证 TS43';
  if (normalized === 'EMAIL' || normalized === 'EMAIL_OTP') return '邮箱';
  if (normalized === 'OAUTH_EMAIL') return 'OAuth 邮箱';
  if (normalized === 'ACCOUNT_TRANSFER' || normalized === 'ACC_TR') return '换绑号码';
  if (normalized === 'RECAPTCHA') return 'reCAPTCHA';
  if (normalized === 'TWO_FACTOR_PIN' || normalized === 'TWOFAC_PIN') return '两步验证 PIN';
  if (normalized === 'PASSWORD') return '密码';
  if (normalized === 'WIPE_FULL') return '完整重置';
  if (normalized === 'WIPE_OFFLINE') return '离线重置';
  if (normalized === 'FLASH') return '未接来电';
  return normalized.replaceAll('_', ' ').toLowerCase().replace(/\b\w/g, (char) => char.toUpperCase());
}

export function accountFlowLabel(value?: string) {
  switch (normalizeStatus(value)) {
    case 'registered': return '旧设备可用';
    case 'not_registered': return '无旧设备记录';
    case 'blocked': return '号码被拒绝';
    case 'invalid_number': return '号码格式异常';
    case 'rate_limited': return '请求冷却中';
    case 'probe_failed': return '检测失败';
    case 'sms_route_unavailable': return '无可用短信通道';
    case 'unknown': return '状态待确认';
    case '': return '';
    default: return '状态待确认';
  }
}

export function accountStatusLabel(value?: string) {
  switch (normalizeStatus(value)) {
    case 'reachable':
    case 'account_probe_status_reachable':
    case 'exists':
    case 'ok':
    case 'sent':
    case 'waiting': return '已受理';
    case 'rejected':
    case 'account_probe_status_rejected': return '请求被拒绝';
    case 'fail':
    case 'failed': return '请求失败';
    case 'incorrect': return '校验未通过';
    case '': return '';
    default: return '状态待确认';
  }
}

export function accountReasonLabel(...values: Array<string | undefined>) {
  const normalized = normalizeStatus(values.filter(Boolean).join(' '));
  if (!normalized) return '';
  if (hasAny(normalized, ['format_wrong'])) return '号码格式不符合 WA 规则';
  if (hasAny(normalized, ['length_short', 'length_long'])) return '号码长度不符合 WA 规则';
  if (hasAny(normalized, ['blocked'])) return '号码被 WA 拒绝或封禁';
  if (hasAny(normalized, ['too_recent', 'too_many', 'temporarily_unavailable', 'rate_limited', 'cooling_down'])) return '请求过于频繁，请稍后再试';
  if (hasAny(normalized, ['no_routes', 'route_unavailable'])) return '暂无可用验证通道';
  if (hasAny(normalized, ['invalid_skey', 'bad_token'])) return '注册会话已失效，请重新检测';
  if (hasAny(normalized, ['missing_param', 'bad_param'])) return '请求参数被 WA 拒绝，请重新检测号码';
  if (hasAny(normalized, ['old_version'])) return '当前客户端版本被 WA 拒绝';
  if (hasAny(normalized, [
    'proxy',
    'dynamic_ip',
    'unreachable',
    'network',
    'timeout',
    'deadline',
    'eof',
    'wasafe',
    'upstream http',
    'http 502',
    'http 503',
    'http 504',
  ])) return '网络或代理出口异常';
  if (hasAny(normalized, ['fail', 'failed', 'rejected'])) return '请求被 WA 拒绝';
  if (hasAny(normalized, ['ok', 'sent', 'waiting'])) return '请求已受理';
  return '远端返回未识别原因';
}

function textOf(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function normalizeStatus(value?: string) {
  return (value || '').trim().toLowerCase().replace(/^wa_account_status_/, '').replace(/^client_profile_status_/, '');
}

function hasAny(value: string, needles: string[]) {
  return needles.some((needle) => value.includes(needle));
}

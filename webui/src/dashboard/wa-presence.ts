import type { WAContactPresence } from '../proto/byte/v/forge/waapp/v1/contacts';
import { WAChatActivityType, WAContactPresenceState } from '../proto/byte/v/forge/waapp/v1/contacts';

export function contactPresenceLabel(presence?: WAContactPresence, now = Date.now()) {
  const active = presence?.activities?.filter((activity) => activity.updated_at && now - Date.parse(activity.updated_at) < 15_000 && (activity.type === WAChatActivityType.WA_CHAT_ACTIVITY_TYPE_TYPING || activity.type === WAChatActivityType.WA_CHAT_ACTIVITY_TYPE_RECORDING)) || [];
  const recording = active.some((activity) => activity.type === WAChatActivityType.WA_CHAT_ACTIVITY_TYPE_RECORDING);
  if (active.length) return recording ? '正在录音…' : '正在输入…';
  if (!presence?.observed_at || now - Date.parse(presence.observed_at) > 300_000) return '';
  if (presence.state === WAContactPresenceState.WA_CONTACT_PRESENCE_STATE_ONLINE) return '在线';
  if (presence.state === WAContactPresenceState.WA_CONTACT_PRESENCE_STATE_OFFLINE && presence.last_seen_at) {
    const date = new Date(presence.last_seen_at);
    if (!Number.isNaN(date.getTime())) return `最后上线于 ${date.toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}`;
  }
  return '';
}

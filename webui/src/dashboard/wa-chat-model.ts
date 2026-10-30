import type { ThreadMessageLike } from '@assistant-ui/react';
import type { WAContact as WAContactRecord } from '../proto/byte/v/forge/waapp/v1/contacts';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { AccountMessage } from '../proto/byte/v/forge/waapp/v1/messaging';
import { MessageContentType, type MessageContent, type MessageReaction } from '../proto/byte/v/forge/waapp/v1/message_content';
import type { GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import { AccountMessageDirection, AccountMessageSource, InboundMessageKind, MessageEncryptionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import { messageStatus, type WaMessageStatus } from './wa-message-status';
import { canonicalContactRef, contactAliasIndex } from './wa-contact-aliases';
import { createWaSenderLookup, waContactName } from './wa-chat-senders';
import { contactProfilePictureURL } from './wa-contact-picture';

export type WaChatEvent = {
  id: string;
  contactID: string;
  text: string;
  at?: Date;
  source: string;
  sender: string;
  senderRef: string;
  senderDetail?: string;
  senderPictureURL?: string;
  copyText?: string;
  outgoing: boolean;
  read: boolean;
  canMarkRead: boolean;
  readAt?: Date;
  status: WaMessageStatus;
  group: boolean;
  content?: MessageContent;
  edited: boolean;
  revoked: boolean;
  reactions: MessageReaction[];
};

export type WaContact = {
  id: string;
  title: string;
  subtitle: string;
  kind: WAContactKind;
  unreadCount: number;
  preview: string;
  lastAt?: Date;
  profilePictureURL?: string;
  statsFromRecord?: boolean;
};

export type WaChatMeta = Pick<WaChatEvent, 'source' | 'sender' | 'senderRef' | 'senderDetail' | 'senderPictureURL' | 'copyText' | 'outgoing' | 'read' | 'canMarkRead' | 'readAt' | 'status' | 'group' | 'id' | 'content' | 'edited' | 'revoked' | 'reactions'> & { displayText: string };

export function buildWaChatEvents(messages: AccountMessage[], records: WAContactRecord[] = [], group?: GroupChat) {
  const senderLabel = createWaSenderLookup(records, group);
  return messages.filter((message) => message.kind === InboundMessageKind.INBOUND_MESSAGE_KIND_MESSAGE).sort(byMessageTime).map(messageEvent).filter(isKnownChatEvent).map((event) => {
    const sender = senderLabel(event.senderRef, event.group);
    return { ...event, sender: sender.name || event.sender, senderDetail: sender.detail, senderPictureURL: sender.pictureURL };
  });
}

export function buildWaContacts(events: WaChatEvent[], records: WAContactRecord[] = []) {
  const contacts = new Map<string, WaContact>();
  const aliases = contactAliasIndex(records);
  for (const record of records) {
    const rawID = recordContactID(record);
    const id = aliases.get(canonicalContactRef(rawID)) || rawID;
    if (!id) continue;
    if (contacts.has(id) && rawID !== id) continue;
    contacts.set(id, { id, title: recordTitle(record), subtitle: recordSubtitle(record), kind: record.kind || WAContactKind.WA_CONTACT_KIND_UNSPECIFIED, unreadCount: record.unread_count || 0, preview: record.last_message_preview || '', lastAt: parseDate(record.last_message_at), profilePictureURL: contactProfilePictureURL(record), statsFromRecord: true });
  }
  for (const event of events) {
    const contactID = aliases.get(canonicalContactRef(event.contactID)) || event.contactID;
    const current = contacts.get(contactID);
    const keepRecordStats = Boolean(current?.statsFromRecord);
    contacts.set(contactID, {
      id: contactID,
      title: current?.title || (event.group ? '群聊' : event.sender),
      subtitle: current?.subtitle || event.source,
      kind: current?.kind || (event.group ? WAContactKind.WA_CONTACT_KIND_GROUP : WAContactKind.WA_CONTACT_KIND_USER),
      unreadCount: keepRecordStats ? current?.unreadCount || 0 : (current?.unreadCount || 0) + (isUnreadChatEvent(event) ? 1 : 0),
      preview: !current?.lastAt || (event.at && event.at >= current.lastAt) ? event.text : current?.preview || event.text,
      lastAt: newerDate(current?.lastAt, event.at),
      profilePictureURL: current?.profilePictureURL,
      statsFromRecord: keepRecordStats,
    });
  }
  return [...contacts.values()].sort(compareContacts);
}

export function toAssistantMessage(event: WaChatEvent): ThreadMessageLike {
  const role = event.outgoing ? 'user' : 'assistant';
  const message: ThreadMessageLike = {
    id: event.id,
    role,
    content: event.text,
    createdAt: event.at,
    metadata: { custom: { source: event.source, sender: event.sender, senderRef: event.senderRef, senderDetail: event.senderDetail, senderPictureURL: event.senderPictureURL, copyText: event.copyText, outgoing: event.outgoing, displayText: event.text, read: event.read, canMarkRead: event.canMarkRead, readAt: event.readAt, status: event.status, group: event.group, id: event.id, content: event.content, edited: event.edited, revoked: event.revoked, reactions: event.reactions } satisfies WaChatMeta },
  };
  return role === 'assistant' ? { ...message, status: { type: 'complete', reason: 'stop' } } : message;
}

export function formatChatTime(value?: Date) {
  if (!value) return '';
  const today = new Date();
  if (value.toDateString() === today.toDateString()) return value.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (value.toDateString() === yesterday.toDateString()) return '昨天';
  return value.toLocaleDateString('zh-CN');
}

function messageEvent(item: AccountMessage): WaChatEvent {
  const text = item.revoked_at ? '这条消息已撤回' : item.text?.value || item.text?.redacted_value || messageStateLabel(item.encryption_state);
  return {
    id: item.account_message_id || item.message_session_id || `${item.wa_account_id}-${item.received_at}`,
    contactID: (item.contact_ref || item.sender_ref || '').trim() || 'unknown',
    text,
    at: parseDate(item.received_at),
    source: messageSourceLabel(item),
    sender: sourcePartyLabel(item.sender_ref || item.contact_ref),
    senderRef: item.sender_ref || item.contact_ref || '',
    copyText: item.revoked_at ? '' : item.text?.value || '',
    outgoing: item.direction === AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_OUTBOUND,
    read: Boolean(item.read_at),
    canMarkRead: item.direction !== AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_OUTBOUND,
    readAt: parseDate(item.read_at),
    status: messageStatus(item),
    group: item.contact_ref?.endsWith('@g.us') || false,
    content: item.content,
    edited: Boolean(item.edited_at),
    revoked: Boolean(item.revoked_at),
    reactions: item.reactions || [],
  };
}

export function isUnreadChatEvent(event: WaChatEvent) {
  return event.canMarkRead && !event.outgoing && !event.read;
}

function newerDate(a?: Date, b?: Date) { return !a ? b : !b ? a : a.getTime() > b.getTime() ? a : b; }

function byMessageTime(a: AccountMessage, b: AccountMessage) {
  return (parseDate(a.received_at)?.getTime() || 0) - (parseDate(b.received_at)?.getTime() || 0);
}

function sourcePartyLabel(value?: string) {
  const raw = canonicalContactRef(value || '');
  if (!raw) return '未知联系人';
  if (raw === 's.whatsapp.net') return '系统';
  if (raw.endsWith('@s.whatsapp.net')) return `+${raw.replace(/@s\.whatsapp\.net$/, '')}`;
  if (raw.endsWith('@lid')) return '未知联系人';
  return raw;
}

function recordContactID(record: WAContactRecord) { return (record.jid || record.number || record.contact_id || '').trim(); }

function recordTitle(record: WAContactRecord) {
  const name = waContactName(record);
  if (record.kind === WAContactKind.WA_CONTACT_KIND_BUSINESS) return name || '企业联系人';
  return name || phoneTitle(record.number) || sourcePartyLabel(record.jid) || '未知联系人';
}

function recordSubtitle(record: WAContactRecord) {
  if (record.kind === WAContactKind.WA_CONTACT_KIND_SYSTEM) return '';
  if (record.kind === WAContactKind.WA_CONTACT_KIND_BUSINESS) return '';
  if (record.kind === WAContactKind.WA_CONTACT_KIND_GROUP) return '群聊';
  if (record.number) return `+${record.number}`;
  return '';
}

function compareContacts(a: WaContact, b: WaContact) {
  const delta = (b.lastAt?.getTime() || 0) - (a.lastAt?.getTime() || 0);
  return delta || a.title.localeCompare(b.title);
}

function isKnownChatEvent(event: WaChatEvent) {
  return event.contactID.trim() !== '' && event.contactID !== 'unknown';
}

function messageSourceLabel(item: AccountMessage) {
  if (item.source === AccountMessageSource.ACCOUNT_MESSAGE_SOURCE_LOCAL_SEND) return '已发送';
  if (item.source === AccountMessageSource.ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY) return '导入历史';
  if (item.encryption_state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED) return '解密失败';
  if (item.encryption_state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_ENCRYPTED) return '待解密';
  return 'WA 消息';
}

function messageStateLabel(state?: MessageEncryptionState) {
  if (state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_ENCRYPTED) return '消息待解密';
  if (state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED) return '消息解密失败';
  return '空消息';
}

function phoneTitle(number?: string) {
  const value = (number || '').trim();
  return value ? `+${value}` : '';
}

function parseDate(value?: string) { const time = value ? new Date(value) : undefined; return time && !Number.isNaN(time.getTime()) ? time : undefined; }

export function canQuoteMessage(event: Pick<WaChatEvent, 'revoked' | 'content'>) {
  return !event.revoked && !event.content?.view_once && event.content?.type === MessageContentType.MESSAGE_CONTENT_TYPE_TEXT;
}

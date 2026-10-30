import { describe, expect, it } from 'vitest';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { AccountMessageDirection, AccountMessageSource, InboundMessageKind, MessageAckStatus, MessageDeleteStatus, MessageEncryptionState, MessageReceiptType, type AccountMessage, type MessageReceipt } from '../proto/byte/v/forge/waapp/v1/messaging';
import { WAContactKind, WAChatActivityType, WAContactPresenceState, type WAContact, type WAContactPresence } from '../proto/byte/v/forge/waapp/v1/contacts';
import { buildWaChatEvents, buildWaContacts, canQuoteMessage, toAssistantMessage } from './wa-chat-model';
import { MessageContentType } from '../proto/byte/v/forge/waapp/v1/message_content';
import { WaMessageContent } from './wa-message-content';
import { messageStatus } from './wa-message-status';
import { contactPresenceLabel } from './wa-presence';
import { canonicalContactRef, contactAliasIndex } from './wa-contact-aliases';
import { GroupMemberRole } from '../proto/byte/v/forge/waapp/v1/groups';

describe('WhatsApp chat projections', () => {
  it('preserves literal JSON, edit and quote metadata without guessing message fields', () => {
    const text = '{"text":"literal JSON","token":"fixture"}';
    const content = { type: MessageContentType.MESSAGE_CONTENT_TYPE_TEXT, quote: { provider_message_id: 'original', sender_ref: 'other', text: { value: 'quoted', redacted_value: '', secret_ref: '' } }, ephemeral: false, view_once: false, change: undefined };
    const [event] = buildWaChatEvents([fixtureMessage({ text: { value: text, redacted_value: '', secret_ref: '' }, content, edited_at: new Date().toISOString(), reactions: [{ sender_ref: 'self', text: '👍', timestamp: undefined }] })]);
    expect(event).toMatchObject({ text, edited: true, revoked: false, content, reactions: [{ text: '👍' }] });
    expect(canQuoteMessage(event)).toBe(true);
    expect(canQuoteMessage({ ...event, content: { ...content, view_once: true } })).toBe(false);
    expect(renderToStaticMarkup(createElement(WaMessageContent, { text }))).toContain('literal JSON');
    expect(renderToStaticMarkup(createElement(WaMessageContent, { text }))).toContain('token');
  });
  it('hides revoked content and copy text even if a stale response still contains it', () => {
    const [event] = buildWaChatEvents([fixtureMessage({ text: { value: 'private original', redacted_value: '', secret_ref: '' }, revoked_at: new Date().toISOString() })]);
    expect(event).toMatchObject({ text: '这条消息已撤回', copyText: '', revoked: true });
    expect(canQuoteMessage(event)).toBe(false);
  });
  it('keeps server acknowledgement separate from delivery and remote read', () => {
    expect(messageStatus(fixtureMessage({ ack_status: MessageAckStatus.MESSAGE_ACK_STATUS_ACKED, read_at: new Date().toISOString() })).state).toBe('sent');
    expect(messageStatus(fixtureMessage({ ack_status: MessageAckStatus.MESSAGE_ACK_STATUS_PENDING })).state).toBe('pending');
    expect(messageStatus(fixtureMessage({ receipts: [receipt('other', MessageReceiptType.MESSAGE_RECEIPT_TYPE_READ), receipt('other', MessageReceiptType.MESSAGE_RECEIPT_TYPE_DELIVERED)] })).state).toBe('read');
  });
  it('counts distinct group readers without claiming everyone read', () => {
    const status = messageStatus(fixtureMessage({ contact_ref: 'fixture@g.us', receipts: [receipt('first', MessageReceiptType.MESSAGE_RECEIPT_TYPE_READ), receipt('first', MessageReceiptType.MESSAGE_RECEIPT_TYPE_PLAYED), receipt('second', MessageReceiptType.MESSAGE_RECEIPT_TYPE_DELIVERED)] }));
    expect(status).toMatchObject({ group: true, readCount: 1, label: '1 人已读' });
  });
  it('builds a group conversation before the contact index is available and keeps the newest preview', () => {
    const events = buildWaChatEvents(['old', 'new'].map((value, index) => fixtureMessage({ account_message_id: value, contact_ref: 'fixture@g.us', kind: InboundMessageKind.INBOUND_MESSAGE_KIND_MESSAGE, direction: AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_INBOUND, encryption_state: MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_DECRYPTED, text: { value, redacted_value: '', secret_ref: '' }, received_at: new Date(1700000000000 + index * 1000).toISOString() })));
    expect(buildWaContacts(events)[0]).toMatchObject({ title: '群聊', kind: WAContactKind.WA_CONTACT_KIND_GROUP, preview: 'new', unreadCount: 2 });
  });
  it('distinguishes group members with the same name using their numbers', () => {
    const senders = ['100001@lid', '100002@lid'];
    const records = senders.map((jid, index) => contact({ jid, display_name: 'WhatsApp', number: `1202555012${index + 3}` }));
    const events = buildWaChatEvents(senders.map((sender_ref, index) => fixtureMessage({ account_message_id: String(index), contact_ref: 'fixture@g.us', sender_ref, direction: AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_INBOUND })), records);
    expect(events.map(event => [event.sender, event.senderDetail])).toEqual([['WhatsApp', '+12025550123'], ['WhatsApp', '+12025550124']]);
    expect(toAssistantMessage(events[0]).metadata?.custom).toMatchObject({ group: true, senderRef: senders[0], senderDetail: '+12025550123' });
  });
  it('keeps distinct member identities while names and group metadata are unavailable', () => {
    const senders = ['100001:2@lid', '100002@lid'];
    const events = buildWaChatEvents(senders.map((sender_ref, index) => fixtureMessage({ account_message_id: String(index), contact_ref: 'fixture@g.us', sender_ref })));
    expect(events.map(event => event.sender)).toEqual(['成员 100001', '成员 100002']);
  });
  it('does not attribute a message with a missing sender to the group name', () => {
    const [event] = buildWaChatEvents([fixtureMessage({ contact_ref: 'fixture@g.us', sender_ref: '' })], [contact({ jid: 'fixture@g.us', display_name: 'Group title', kind: WAContactKind.WA_CONTACT_KIND_GROUP })]);
    expect(event.sender).toBe('未知成员');
    expect(event.senderPictureURL).toBeUndefined();
  });
  it('attributes group avatars to each sender and preserves the cached image URL in message metadata', () => {
    const records = [
      contact({ contact_id: 'alice-row', jid: '100001@lid', display_name: 'Alice', profile_picture_id: 'alice-picture' }),
      contact({ contact_id: 'bob-row', jid: '100002@lid', display_name: 'Bob', profile_picture_id: 'bob-picture' }),
      contact({ contact_id: 'group-row', jid: 'fixture@g.us', display_name: 'Group', kind: WAContactKind.WA_CONTACT_KIND_GROUP, profile_picture_id: 'group-picture' }),
    ];
    const events = buildWaChatEvents(['100001:2@lid', '100002@lid'].map((sender_ref, index) => fixtureMessage({ account_message_id: String(index), contact_ref: 'fixture@g.us', sender_ref })), records);
    expect(events.map(event => event.senderPictureURL)).toEqual([
      '/api/wa/contacts/alice-row/profile-picture?v=alice-picture',
      '/api/wa/contacts/bob-row/profile-picture?v=bob-picture',
    ]);
    expect(toAssistantMessage(events[0]).metadata?.custom).toMatchObject({ senderPictureURL: events[0].senderPictureURL });
  });
  it('resolves a group member through the phone contact without a LID contact row', () => {
    const [event] = buildWaChatEvents([fixtureMessage({ contact_ref: 'fixture@g.us', sender_ref: '100001:3@lid' })], [contact({ contact_id: 'phone-row', jid: '12025550123@s.whatsapp.net', display_name: 'Alice', profile_picture_id: 'picture-v2' })], { group_ref: 'fixture@g.us', name: 'Fixture', members: [{ member_ref: '100001@lid', phone_ref: '12025550123@s.whatsapp.net', role: GroupMemberRole.GROUP_MEMBER_ROLE_MEMBER }], description: '', description_id: '', announcement_only: false, locked: false, all_members_can_add: false, join_approval: false, self_member_ref: '', member_count: 1 });
    expect(event).toMatchObject({ sender: 'Alice', senderDetail: '+12025550123', senderPictureURL: '/api/wa/contacts/phone-row/profile-picture?v=picture-v2' });
  });
  it('uses an available alias picture when the sender row has no picture version', () => {
    const phone = contact({ contact_id: 'phone-row', jid: '12025550123@s.whatsapp.net', number: '12025550123', profile_picture_id: 'known-picture' });
    const lid = contact({ contact_id: 'lid-row', jid: '100001@lid', number: '+12025550123' });
    for (const records of [[phone, lid], [lid, phone]]) {
      const [event] = buildWaChatEvents([fixtureMessage({ contact_ref: 'fixture@g.us', sender_ref: lid.jid })], records);
      expect(event.senderPictureURL).toBe('/api/wa/contacts/phone-row/profile-picture?v=known-picture');
    }
  });
  it('keeps a saved contact name when alias rows contain only a push name or placeholder', () => {
    const phone = contact({ jid: '12025550123@s.whatsapp.net', number: '12025550123', display_name: 'Alice' });
    const lid = contact({ jid: '100001@lid', number: '+12025550123', display_name: '未知联系人', wa_name: 'Push name' });
    for (const records of [[phone, lid], [lid, phone]]) {
      const [event] = buildWaChatEvents([fixtureMessage({ contact_ref: 'fixture@g.us', sender_ref: lid.jid })], records);
      expect(event).toMatchObject({ sender: 'Alice', senderDetail: '+12025550123' });
    }
  });
  it('expires typing and does not fabricate private presence', () => {
    const now = Date.now();
    expect(contactPresenceLabel(presence({ activities: [{ participant_ref: 'fixture', type: WAChatActivityType.WA_CHAT_ACTIVITY_TYPE_TYPING, updated_at: new Date(now).toISOString() }] }), now)).toBe('正在输入…');
    expect(contactPresenceLabel(presence({ activities: [{ participant_ref: 'fixture', type: WAChatActivityType.WA_CHAT_ACTIVITY_TYPE_TYPING, updated_at: new Date(now - 16000).toISOString() }] }), now)).toBe('');
    expect(contactPresenceLabel(presence({ state: WAContactPresenceState.WA_CONTACT_PRESENCE_STATE_OFFLINE, observed_at: new Date(now).toISOString() }), now)).toBe('');
  });
  it('merges phone and LID rows into one conversation with shared routes and message previews', () => {
    const phone = contact({ contact_id: 'phone-row', jid: '15550000001@s.whatsapp.net', number: '15550000001', unread_count: 1 });
    const lid = contact({ contact_id: 'lid-row', jid: '100001@lid', number: '15550000001', display_name: 'Fixture', unread_count: 3 });
    const events = buildWaChatEvents([fixtureMessage({ contact_ref: phone.jid, text: { value: 'Reply', redacted_value: '', secret_ref: '' } })]);
    for (const records of [[phone, lid], [lid, phone]]) {
      expect(buildWaContacts(events, records)).toMatchObject([{ id: lid.jid, title: 'Fixture', unreadCount: 3, preview: 'Reply' }]);
      const aliases = contactAliasIndex(records);
      expect(aliases.get(phone.jid)).toBe(lid.jid);
      expect(aliases.get(phone.contact_id)).toBe(lid.jid);
      expect(aliases.get(canonicalContactRef('+15550000001:4@s.whatsapp.net'))).toBe(lid.jid);
    }
  });
});

function fixtureMessage(overrides: Partial<AccountMessage>): AccountMessage {
  return { account_message_id: 'fixture', wa_account_id: 'account', message_session_id: 'session', contact_ref: 'other', sender_ref: 'other', direction: AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_OUTBOUND, source: AccountMessageSource.ACCOUNT_MESSAGE_SOURCE_LOCAL_SEND, kind: InboundMessageKind.INBOUND_MESSAGE_KIND_MESSAGE, encryption_state: MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_DECRYPTED, ack_status: MessageAckStatus.MESSAGE_ACK_STATUS_ACKED, text: undefined, received_at: undefined, last_error: undefined, read_at: undefined, delete_status: MessageDeleteStatus.MESSAGE_DELETE_STATUS_NOT_DELETED, deleted_at: undefined, provider_message_id: 'fixture', receipts: [], read_receipt_sent_at: undefined, content: undefined, edited_at: undefined, revoked_at: undefined, reactions: [], ...overrides };
}
function receipt(recipient: string, type: MessageReceiptType): MessageReceipt { return { provider_message_id: 'fixture', contact_ref: 'other', recipient_ref: recipient, type, timestamp: undefined }; }
function presence(overrides: Partial<WAContactPresence>): WAContactPresence { return { contact_ref: 'other', state: WAContactPresenceState.WA_CONTACT_PRESENCE_STATE_UNSPECIFIED, last_seen_at: undefined, observed_at: undefined, activities: [], ...overrides }; }
function contact(overrides: Partial<WAContact>): WAContact { return { contact_id: '', wa_account_id: 'account', jid: '', number: '', display_name: '', wa_name: '', verified_name: '', kind: WAContactKind.WA_CONTACT_KIND_USER, is_whatsapp_user: true, is_reachable: true, audit: undefined, profile_picture_id: '', message_count: 0, unread_count: 0, last_message_at: undefined, last_message_preview: '', saved_name: '', ...overrides }; }

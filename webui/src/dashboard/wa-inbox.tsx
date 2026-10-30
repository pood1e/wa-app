import { useMemo } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import { WAAccountStatus, type WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { deleteWaContact, getWaAccountOtpMessages, getWaContacts, getWaMessages, subscribeWaPresence, sendWaTextMessage, editWaMessage, reactToWaMessage, revokeWaMessage, waAccountID, waKeys } from './wa-api';
import { useWaContactAutoResolve } from './wa-contact-resolve';
import { buildWaChatEvents, buildWaContacts } from './wa-chat-model';
import { WaChatThread } from './wa-chat-thread';
import { WaContactList } from './wa-contact-list';
import { contactPresenceLabel } from './wa-presence';
import { useWaReadMessages } from './wa-read-messages';
import { canonicalContactRef, contactAliasIndex } from './wa-contact-aliases';
import { isConnectionRejected } from './wa-long-connection-badge';
import { useWaGroup } from './wa-group-query';
import { groupPermissions } from './wa-group-permissions';
import { WaGroupDetails } from './wa-group-details';

export function WaInbox({ account, contactID, connection }: { account: WAAccount; contactID: string; connection?: LongConnectionState }) {
  const accountID = waAccountID(account);
  const readOnly = account.status !== WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE;
  const online = !readOnly && (connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING);
  const queryClient = useQueryClient();
  const contactsQuery = useQuery({ queryKey: waKeys.contacts(accountID), queryFn: ({ signal }) => getWaContacts(accountID, '', signal), enabled: Boolean(accountID), refetchInterval: 30000 });
  useWaContactAutoResolve(online ? accountID : '', contactsQuery.data?.contacts || []);
  const activeContactID = contactID;
  const messagesQuery = useInfiniteQuery({ queryKey: waKeys.messages(accountID, activeContactID), queryFn: ({ pageParam, signal }) => getWaMessages(accountID, activeContactID, pageParam, signal), initialPageParam: '', getNextPageParam: (page) => page.next_cursor || undefined, enabled: Boolean(accountID && activeContactID), refetchInterval: 5000 });
  const presenceQuery = useQuery({ queryKey: waKeys.presence(accountID, activeContactID), queryFn: ({ signal }) => subscribeWaPresence(accountID, activeContactID, signal), enabled: Boolean(accountID && activeContactID && online), refetchInterval: 3000, retry: false });
  const groupQuery = useWaGroup(accountID, activeContactID, online);
  const otpQuery = useQuery({ queryKey: waKeys.otpMessages(accountID), queryFn: ({ signal }) => getWaAccountOtpMessages(accountID, { limit: 5, includeSensitiveValues: true, signal }), enabled: Boolean(accountID), refetchInterval: 5000 });
  const messages = useMemo(() => [...new Map(messagesQuery.data?.pages.slice().reverse().flatMap((page) => page.messages || []).map((message) => [message.account_message_id, message])).values()], [messagesQuery.data]);
  const events = useMemo(() => buildWaChatEvents(messages, contactsQuery.data?.contacts, groupQuery.data?.group), [messages, contactsQuery.data?.contacts, groupQuery.data?.group]);
  const contacts = useMemo(() => buildWaContacts(events, contactsQuery.data?.contacts || []), [events, contactsQuery.data?.contacts]);
  const aliases = useMemo(() => contactAliasIndex(contactsQuery.data?.contacts || []), [contactsQuery.data?.contacts]);
  const selectedContactID = aliases.get(canonicalContactRef(activeContactID)) || activeContactID;
  const recordContact = contacts.find((contact) => contact.id === selectedContactID);
  const group = groupQuery.data?.group;
  const selfPhoneRef = `${(account.phone?.e164_number || '').replace(/^\+/, '')}@s.whatsapp.net`;
  const selfRef = aliases.get(selfPhoneRef) || selfPhoneRef;
  const permissions = groupPermissions(group);
  let groupUnavailable = '';
  if (activeContactID.endsWith('@g.us')) {
    if (groupQuery.error) groupUnavailable = '群资料暂时无法读取，请在群资料中刷新后重试。';
    else if (!group) groupUnavailable = online ? '正在确认群成员身份…' : '连接恢复后可确认群成员身份。';
    else if (!permissions.member) groupUnavailable = '你已不在此群聊中，历史消息仍可查看。';
    else if (!permissions.send) groupUnavailable = '仅管理员可在此群聊发送消息。';
  }
  const presence = online ? contactPresenceLabel(presenceQuery.data?.presence) : '';
  const activeContact = activeContactID ? { ...(recordContact || { id: activeContactID, title: activeContactID.endsWith('@g.us') ? '群聊' : activeContactID.endsWith('@lid') ? '联系人' : activeContactID.split('@')[0], kind: activeContactID.endsWith('@g.us') ? WAContactKind.WA_CONTACT_KIND_GROUP : WAContactKind.WA_CONTACT_KIND_USER, unreadCount: 0, preview: '', subtitle: '' }), title: group?.name || recordContact?.title || (activeContactID.endsWith('@g.us') ? '群聊' : activeContactID.split('@')[0]), subtitle: presence || (group?.members?.length ? `${group.members.length} 位成员` : recordContact?.subtitle || '') } : undefined;
  const markReadMutation = useWaReadMessages(accountID, activeContactID, messages, !online, messagesQuery.dataUpdatedAt);
  const activeOtp = useMemo(() => latestActiveOtp(otpQuery.data?.otp_messages || []), [otpQuery.data?.otp_messages]);
  const threadEvents = events;
  const refreshMessageViews = async (messageContactID = activeContactID) => {
    await Promise.all([queryClient.invalidateQueries({ queryKey: waKeys.messages(accountID, messageContactID) }), queryClient.invalidateQueries({ queryKey: waKeys.contacts(accountID) }), queryClient.invalidateQueries({ queryKey: waKeys.otpMessages(accountID) })]);
  };
  const deleteContactMutation = useMutation({
    mutationFn: async (deleteContactID: string) => {
      const resp = await deleteWaContact(accountID, deleteContactID);
      if (resp.error?.message) throw new Error(resp.error.message);
      return resp;
    },
    onSettled: () => refreshMessageViews(),
  });
  const sendMutation = useMutation({
    mutationFn: async (input: { text: string; contactID: string; replyTo?: string }) => {
      const resp = await sendWaTextMessage(accountID, input.contactID, input.text, input.replyTo);
      if (resp.error?.message) throw new Error(resp.error.message);
      return resp;
    },
    onSettled: (_data, _error, input) => refreshMessageViews(input.contactID),
  });
  const actionMutation = useMutation({
    mutationFn: async (input: { type: 'edit' | 'revoke' | 'react'; id: string; text?: string; contactID: string }) => {
      if (input.type === 'edit') return editWaMessage(accountID, input.id, input.text || '');
      if (input.type === 'react') return reactToWaMessage(accountID, input.id, input.text || '');
      return revokeWaMessage(accountID, input.id);
    },
    onSettled: (_data, _error, input) => refreshMessageViews(input.contactID),
  });
  const currentSend = sendMutation.variables?.contactID === activeContactID;
  const error = [messagesQuery.error, markReadMutation.error, currentSend ? sendMutation.error : null, actionMutation.variables?.contactID === activeContactID ? actionMutation.error : null].map(errorMessage).find(Boolean);
  return (
    <section className="grid h-dvh min-h-0 md:grid-cols-[minmax(280px,34%)_minmax(0,1fr)] xl:grid-cols-[380px_minmax(0,1fr)]">
      <div className={`min-h-0 min-w-0 ${contactID ? 'hidden md:block' : ''}`}>
        <WaContactList key={accountID} accountID={accountID} selfRef={selfRef} contacts={contacts} selectedID={selectedContactID} loading={contactsQuery.isLoading} readOnly={!online} error={errorMessage(contactsQuery.error) || errorMessage(deleteContactMutation.error)} deletingID={deleteContactMutation.isPending ? deleteContactMutation.variables : undefined} otp={activeOtp} onDeleteContact={(id) => deleteContact(id, deleteContactMutation.mutate)} />
      </div>
      <div className={`min-h-0 min-w-0 ${contactID ? '' : 'hidden md:block'}`}>
        <WaChatThread key={`${accountID}/${activeContactID}`} accountID={accountID} contact={activeContact} events={threadEvents} loading={messagesQuery.isLoading} sending={currentSend && sendMutation.isPending} readOnly={readOnly} offline={!online} rejected={isConnectionRejected(connection)} error={error} hasOlder={messagesQuery.hasNextPage} loadingOlder={messagesQuery.isFetchingNextPage} onLoadOlder={() => messagesQuery.fetchNextPage()} onRetry={() => messagesQuery.refetch()} onSendMessage={(text, replyTo) => sendMutation.mutateAsync({ text, replyTo, contactID: activeContactID })} onMessageAction={(type, id, text) => actionMutation.mutateAsync({ type, id, text, contactID: activeContactID })} acting={actionMutation.isPending && actionMutation.variables?.contactID === activeContactID} unavailableReason={groupUnavailable} headerActions={activeContactID.endsWith('@g.us') ? <WaGroupDetails accountID={accountID} groupRef={activeContactID} records={contactsQuery.data?.contacts || []} contacts={contacts} online={online} pictureURL={activeContact?.profilePictureURL} /> : undefined} />
      </div>
    </section>
  );
}

function deleteContact(contactID: string, mutate: (contactID: string) => void) {
  if (!contactID) return;
  if (window.confirm('删除该联系人和本地会话？')) mutate(contactID);
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '';
}

function latestActiveOtp(messages: OtpMessage[]) {
  const now = Date.now();
  return messages.find((message) => {
    const expiresAt = Date.parse(message.expires_at || '');
    const code = message.otp?.value || message.otp?.redacted_value || '';
    return Boolean(code) && Number.isFinite(expiresAt) && expiresAt > now;
  });
}

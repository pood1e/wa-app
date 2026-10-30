import type { GetTwoFactorAuthStatusResponse, RemoveAccountProfilePictureResponse, RequestAccountEmailOtpResponse, SetAccountEmailResponse, SetAccountProfileNameResponse, SetAccountProfilePictureResponse, SetTwoFactorAuthSettingsResponse, VerifyAccountEmailOtpResponse } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { AccountReviewResponse, SubmitAccountReviewRequest } from '../proto/byte/v/forge/waapp/v1/account_review';
import type { AccountProxy, AccountProxyResponse } from '../proto/byte/v/forge/waapp/v1/account_proxy';
import type { DeleteWAContactResponse, ListWAContactsResponse, ResolveWAContactsResponse, SaveWAContactRequest, SaveWAContactResponse, SubscribeWAContactPresenceResponse } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { GroupResponse } from '../proto/byte/v/forge/waapp/v1/groups';
import type { ListAccountOtpMessagesResponse } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { DeleteAccountMessagesResponse, GetLongConnectionStatusResponse, ListAccountMessagesResponse, LongConnectionState, MarkAccountMessagesReadResponse, SendTextMessageResponse, EditAccountMessageRequest, ReactToAccountMessageRequest } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { DeleteWAAccountResponse, ListWAAccountsResponse, LogoutWAAccountRequest, LogoutWAAccountResponse, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';

export const ACCOUNT_PAGE_SIZE = 100;

export type WaConnectionState = LongConnectionState;
export type WaConnectionFilters = { login_state_id?: string; wa_account_id?: string; client_profile_id?: string; registered_identity_id?: string };
export type WaAccountProjection = WAAccount;

export const waKeys = {
  accounts: () => ['wa', 'accounts'] as const,
  review: (waAccountId: string) => ['wa', 'review', waAccountId] as const,
  proxy: (waAccountId: string) => ['wa', 'proxy', waAccountId] as const,
  messages: (waAccountId: string, contactRef = '') => ['wa', 'messages', waAccountId, contactRef] as const,
  contacts: (waAccountId: string) => ['wa', 'contacts', waAccountId] as const,
  contactResolve: (waAccountId: string) => ['wa', 'contacts', 'resolve', waAccountId] as const,
  presence: (waAccountId: string, contact: string) => ['wa', 'presence', waAccountId, contact] as const,
  group: (waAccountId: string, contact: string) => ['wa', 'group', waAccountId, contact] as const,
  groupRequests: (waAccountId: string, contact: string) => ['wa', 'group-requests', waAccountId, contact] as const,
  twoFactorStatus: (waAccountId: string) => ['wa', '2fa-status', waAccountId] as const,
  otpMessages: (waAccountId: string) => ['wa', 'otp-messages', waAccountId] as const,
  connections: (filters: WaConnectionFilters = {}) => ['wa', 'connections', filters.login_state_id || '', filters.wa_account_id || '', filters.client_profile_id || '', filters.registered_identity_id || ''] as const,
};

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method?.toUpperCase() || 'GET';
  const timeout = AbortSignal.timeout(method === 'GET' || method === 'HEAD' ? 10_000 : 100_000);
  const signal = init?.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  try {
    const resp = await fetch(path, { ...init, signal, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) } });
    if (resp.status === 401) redirectToLogin();
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    return await resp.json() as T;
  } catch (error) {
    if (init?.signal?.aborted) throw error;
    if (timeout.aborted) throw new Error('连接服务超时，请检查网络后重试', { cause: error });
    if (error instanceof TypeError) throw new Error('无法连接服务，请检查网络后重试', { cause: error });
    throw error;
  }
}

function redirectToLogin() {
  const next = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  window.location.assign(`/login?next=${encodeURIComponent(next || '/')}`);
}

export function getWaConnections(filters: WaConnectionFilters = {}, signal?: AbortSignal) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value);
  return getWaResponse<GetLongConnectionStatusResponse>(`/api/wa/long-connections${params.size ? `?${params}` : ''}`, signal);
}

export function getWaAccountProxy(accountID: string, signal?: AbortSignal) {
  return getWaResponse<AccountProxyResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}/proxy`, signal);
}

export function setWaAccountProxy(accountID: string, proxy: AccountProxy | undefined) {
  return mutateWaResponse<AccountProxyResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}/proxy`, { method: 'PUT', body: JSON.stringify({ proxy }) });
}

export async function getWaAccounts(cursor = '', signal?: AbortSignal) {
  const params = new URLSearchParams({ limit: String(ACCOUNT_PAGE_SIZE) });
  if (cursor) params.set('cursor', cursor);
  const response = await getWaResponse<ListWAAccountsResponse>(`/api/wa/accounts?${params}`, signal);
  const accounts = Array.isArray(response.accounts) ? response.accounts.filter((account): account is WAAccount => Boolean(account)) : [];
  return { ...response, accounts };
}

export function getWaAccountOtpMessages(waAccountId: string, options: { cursor?: string; limit?: number; includeSensitiveValues?: boolean; signal?: AbortSignal } = {}) {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: String(options.limit || 20) });
  if (options.cursor) params.set('cursor', options.cursor);
  if (options.includeSensitiveValues !== undefined) params.set('include_sensitive_values', String(options.includeSensitiveValues));
  return getWaResponse<ListAccountOtpMessagesResponse>(`/api/wa/account-otp-messages?${params}`, options.signal);
}


export type MarkWaMessagesReadInput = { accountMessageIds?: string[]; contactRef?: string; localOnly?: boolean };

export function markWaMessagesRead(waAccountId: string, input: MarkWaMessagesReadInput) {
  return mutateWaResponse<MarkAccountMessagesReadResponse>('/api/wa/messages/read', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: input.accountMessageIds || [], contact_ref: input.contactRef || '', local_only: Boolean(input.localOnly) }) });
}

export function deleteWaMessagesForMe(waAccountId: string, accountMessageIds: string[]) {
  return mutateWaResponse<DeleteAccountMessagesResponse>('/api/wa/messages/delete', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: accountMessageIds, mode: 'for_me' }) });
}

export function sendWaTextMessage(waAccountId: string, contactRef: string, text: string, replyTo = '') {
  return mutateWaResponse<SendTextMessageResponse>('/api/wa/messages/send', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, contact_ref: contactRef, text, reply_to_account_message_id: replyTo }) });
}

export function editWaMessage(waAccountId: string, messageID: string, text: string) {
  const request: EditAccountMessageRequest = { context: undefined, wa_account_id: waAccountId, account_message_id: messageID, text: { value: text, redacted_value: '', secret_ref: '' }, client_message_id: '' };
  return mutateWaResponse<SendTextMessageResponse>('/api/wa/messages/edit', { method: 'POST', body: JSON.stringify(request) });
}

export function reactToWaMessage(waAccountId: string, messageID: string, text: string) {
  const request: ReactToAccountMessageRequest = { context: undefined, wa_account_id: waAccountId, account_message_id: messageID, text, client_message_id: '' };
  return mutateWaResponse<SendTextMessageResponse>('/api/wa/messages/react', { method: 'POST', body: JSON.stringify(request) });
}

export function revokeWaMessage(waAccountId: string, messageID: string) {
  return mutateWaResponse<DeleteAccountMessagesResponse>('/api/wa/messages/delete', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: [messageID], mode: 'for_everyone' }) });
}

export function getWaMessages(waAccountId: string, contactRef: string, cursor = '', signal?: AbortSignal) {
  const params = new URLSearchParams({ wa_account_id: waAccountId, contact_ref: contactRef, limit: '100', include_sensitive_text: 'true' });
  if (cursor) params.set('cursor', cursor);
  return getWaResponse<ListAccountMessagesResponse>(`/api/wa/messages?${params}`, signal);
}

export function getWaContacts(waAccountId: string, cursor = '', signal?: AbortSignal) {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '500' });
  if (cursor) params.set('cursor', cursor);
  return getWaResponse<ListWAContactsResponse>(`/api/wa/contacts?${params}`, signal);
}

export function resolveWaContacts(waAccountId: string, jids: string[], signal?: AbortSignal) {
  return mutateWaResponse<ResolveWAContactsResponse>('/api/wa/contacts/resolve', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, jids, limit: jids.length }), signal });
}

export function saveWaContact(request: SaveWAContactRequest, signal?: AbortSignal) {
  return mutateWaResponse<SaveWAContactResponse>('/api/wa/contacts', { method: 'POST', body: JSON.stringify(request), signal });
}

export function subscribeWaPresence(waAccountId: string, contactRef: string, signal?: AbortSignal) {
  return api<SubscribeWAContactPresenceResponse>('/api/wa/contacts/presence', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, contact_ref: contactRef }), signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000) });
}

export function getWaGroup(waAccountId: string, groupRef: string, signal?: AbortSignal) {
  const params = new URLSearchParams({ wa_account_id: waAccountId, group_ref: groupRef });
  return getWaResponse<GroupResponse>(`/api/wa/groups?${params}`, signal);
}

export function deleteWaContact(waAccountId: string, contactID: string) {
  const params = new URLSearchParams({ wa_account_id: waAccountId });
  return mutateWaResponse<DeleteWAContactResponse>(`/api/wa/contacts/${encodeURIComponent(contactID)}?${params}`, { method: 'DELETE' });
}

export async function deleteWaAccount(account: WAAccount | string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const resp = await api<DeleteWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}`, { method: 'DELETE' });
  if (!resp.success || resp.error?.message) throw new Error(resp.error?.message || 'delete WAAccount failed');
  return resp;
}

export function logoutWaAccount(account: WAAccount) {
  const request: LogoutWAAccountRequest = { context: undefined, wa_account_id: waAccountID(account) };
  return mutateWaResponse<LogoutWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(request.wa_account_id)}/logout`, { method: 'POST', body: JSON.stringify(request) });
}

export function getWaAccountReview(accountID: string, signal?: AbortSignal) {
  return getWaResponse<AccountReviewResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}/review`, signal);
}

export function refreshWaAccountReview(accountID: string) {
  return mutateWaResponse<AccountReviewResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}/review/refresh`, { method: 'POST' });
}

export function submitWaAccountReview(accountID: string, description: string) {
  const request: SubmitAccountReviewRequest = { context: undefined, wa_account_id: accountID, description: { value: description, redacted_value: '', secret_ref: '' } };
  return mutateWaResponse<AccountReviewResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}/review`, { method: 'POST', body: JSON.stringify(request) });
}



export async function getWaTwoFactorAuthStatus(account: WAAccount, input: { remoteRefresh?: boolean; signal?: AbortSignal } = {}) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const params = new URLSearchParams({ wa_account_id: accountID });
  if (input.remoteRefresh) params.set('remote_refresh', 'true');
  return requireAccountSettingsResponse(await api<GetTwoFactorAuthStatusResponse>(`/api/wa/account-settings/2fa/status?${params}`, { signal: input.signal }));
}


export async function setWaTwoFactorAuthSettings(account: WAAccount, password: string) {
  return requireAccountSettingsResponse(await api<SetTwoFactorAuthSettingsResponse>('/api/wa/account-settings/2fa', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), password }) }));
}
export async function setWaAccountEmail(account: WAAccount, input: { email_address: string; google_id_token?: string }) {
  return requireAccountSettingsResponse(await api<SetAccountEmailResponse>('/api/wa/account-settings/email', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), email_address: input.email_address, google_id_token: input.google_id_token || '' }) }));
}
export async function requestWaAccountEmailOtp(account: WAAccount) {
  return requireAccountSettingsResponse(await api<RequestAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/request', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), locale_language: 'en', locale_country: 'US' }) }));
}
export async function verifyWaAccountEmailOtp(account: WAAccount, code: string) {
  return requireAccountSettingsResponse(await api<VerifyAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/verify', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), code }) }));
}
export async function setWaAccountProfileName(account: WAAccount, displayName: string) {
  return requireAccountSettingsResponse(await api<SetAccountProfileNameResponse>('/api/wa/account-settings/profile/name', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account), display_name: displayName }) }));
}
export async function setWaAccountProfilePicture(account: WAAccount, input: { image_base64: string; content_type: string }) {
  return requireAccountSettingsResponse(await api<SetAccountProfilePictureResponse>('/api/wa/account-settings/profile/picture', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account), image: input.image_base64, content_type: input.content_type }) }));
}
export async function removeWaAccountProfilePicture(account: WAAccount) {
  return requireAccountSettingsResponse(await api<RemoveAccountProfilePictureResponse>('/api/wa/account-settings/profile/picture/remove', { method: 'POST', body: JSON.stringify({ selector: waAccountSettingsSelector(account) }) }));
}

export const waAccountID = (account?: WAAccount) => account?.wa_account_id || '';
export const waAccountTitle = (account?: WAAccount) => account?.display_name?.trim() || account?.phone?.e164_number || waAccountID(account) || '-';
export function waAccountProfilePictureURL(account: WAAccount | string, version = 'latest') {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  return accountID ? `/api/wa/accounts/${encodeURIComponent(accountID)}/profile-picture?v=${encodeURIComponent(version)}` : '';
}

function waAccountSettingsPayload(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return { wa_account_id: accountID };
}
function waAccountSettingsSelector(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return { wa_account_id: accountID };
}
function requireAccountSettingsResponse<T extends { error?: { message?: string }; operation?: { error?: { message?: string } } }>(resp: T) {
  const message = resp.error?.message || resp.operation?.error?.message;
  if (message) throw new Error(message);
  return resp;
}
export function getWaResponse<T extends { error?: { message?: string } }>(path: string, signal?: AbortSignal) {
  return api<T>(path, { signal }).then(requireWaResponse);
}
export function mutateWaResponse<T extends { error?: { message?: string } }>(path: string, init: RequestInit) {
  return api<T>(path, init).then(requireWaResponse);
}
function requireWaResponse<T extends { error?: { message?: string } }>(resp: T) {
  if (resp.error?.message) throw new Error(resp.error.message);
  return resp;
}

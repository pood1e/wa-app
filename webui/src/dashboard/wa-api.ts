import type { GetTwoFactorAuthStatusResponse, RemoveAccountProfilePictureResponse, RequestAccountEmailOtpResponse, SetAccountEmailResponse, SetAccountProfileNameResponse, SetAccountProfilePictureResponse, SetTwoFactorAuthSettingsResponse, VerifyAccountEmailOtpResponse } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { DeleteWAContactResponse, ListWAContactsResponse, ResolveWAContactsResponse } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { ListAccountOtpMessagesResponse } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { DeleteAccountMessagesResponse, GetLongConnectionStatusResponse, ListAccountMessagesResponse, LongConnectionState, MarkAccountMessagesReadResponse, SendTextMessageResponse } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { DeleteWAAccountResponse, ListClientProfilesResponse, ListWAAccountsResponse, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';

export const ACCOUNT_PAGE_SIZE = 100;

export type WaPhoneInput = { region: string; phone: string; e164_number: string; country_calling_code: string; country_iso2: string };
export type WaWorkflowResponse = { success?: boolean; passed?: boolean; request_failed?: boolean; status?: string; error_message?: string; reject_reason?: string; wa_account_id?: string; client_profile_id?: string; protocol_profile_id?: string; verification_request_id?: string; registration_phase?: string; phone_status?: Record<string, unknown>; account_probe?: Record<string, unknown>; sms_probe?: Record<string, unknown>; phone?: Record<string, unknown>; proxy?: Record<string, unknown>; registration?: Record<string, unknown>; login_state?: Record<string, unknown>; check?: Record<string, unknown> };
export type WaConnectionState = LongConnectionState;
export type WaConnectionFilters = { login_state_id?: string; wa_account_id?: string; client_profile_id?: string; registered_identity_id?: string };
export type WaAccountProjection = WAAccount;

export const waKeys = {
  accounts: () => ['wa', 'accounts'] as const,
  profiles: (waAccountId: string) => ['wa', 'profiles', waAccountId] as const,
  messages: (waAccountId: string, contactRef = '') => ['wa', 'messages', waAccountId, contactRef] as const,
  contacts: (waAccountId: string) => ['wa', 'contacts', waAccountId] as const,
  contactResolve: (waAccountId: string) => ['wa', 'contacts', 'resolve', waAccountId] as const,
  twoFactorStatus: (waAccountId: string) => ['wa', '2fa-status', waAccountId] as const,
  otpMessages: (waAccountId: string) => ['wa', 'otp-messages', waAccountId] as const,
  connections: (filters: WaConnectionFilters = {}) => ['wa', 'connections', filters.login_state_id || '', filters.wa_account_id || '', filters.client_profile_id || '', filters.registered_identity_id || ''] as const,
};

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, { ...init, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) } });
  if (resp.status === 401) redirectToLogin();
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
  return resp.json() as Promise<T>;
}

function redirectToLogin() {
  const next = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  window.location.assign(`/login?next=${encodeURIComponent(next || '/')}`);
}

export function getWaConnections(filters: WaConnectionFilters = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value);
  return api<GetLongConnectionStatusResponse>(`/api/wa/long-connections${params.size ? `?${params}` : ''}`);
}

export async function getWaAccounts(cursor = '') {
  const params = new URLSearchParams({ limit: String(ACCOUNT_PAGE_SIZE) });
  if (cursor) params.set('cursor', cursor);
  const resp = await api<ListWAAccountsResponse>(`/api/wa/accounts?${params}`);
  const accounts: WAAccount[] = Array.isArray(resp.accounts) ? resp.accounts.filter((account): account is WAAccount => Boolean(account)) : [];
  return { ...resp, accounts };
}

export function getWaAccountOtpMessages(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return api<ListAccountOtpMessagesResponse>(`/api/wa/account-otp-messages?${params}`);
}

export function getWaClientProfiles(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return api<ListClientProfilesResponse>(`/api/wa/client-profiles?${params}`);
}

export type MarkWaMessagesReadInput = { accountMessageIds?: string[]; contactRef?: string; localOnly?: boolean };

export function markWaMessagesRead(waAccountId: string, input: MarkWaMessagesReadInput) {
  return api<MarkAccountMessagesReadResponse>('/api/wa/messages/read', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: input.accountMessageIds || [], contact_ref: input.contactRef || '', local_only: Boolean(input.localOnly) }) });
}

export function deleteWaMessagesForMe(waAccountId: string, accountMessageIds: string[]) {
  return api<DeleteAccountMessagesResponse>('/api/wa/messages/delete', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, account_message_ids: accountMessageIds, mode: 'for_me' }) });
}

export function sendWaTextMessage(waAccountId: string, contactRef: string, text: string) {
  return api<SendTextMessageResponse>('/api/wa/messages/send', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, contact_ref: contactRef, text }) });
}

export function getWaMessages(waAccountId: string, contactRef: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, contact_ref: contactRef, limit: '100', include_sensitive_text: 'true' });
  if (cursor) params.set('cursor', cursor);
  return api<ListAccountMessagesResponse>(`/api/wa/messages?${params}`);
}

export function getWaContacts(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '500' });
  if (cursor) params.set('cursor', cursor);
  return api<ListWAContactsResponse>(`/api/wa/contacts?${params}`);
}

export function resolveWaContacts(waAccountId: string, jids: string[]) {
  return api<ResolveWAContactsResponse>('/api/wa/contacts/resolve', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountId, jids, limit: jids.length }) });
}

export function deleteWaContact(waAccountId: string, contactID: string) {
  const params = new URLSearchParams({ wa_account_id: waAccountId });
  return api<DeleteWAContactResponse>(`/api/wa/contacts/${encodeURIComponent(contactID)}?${params}`, { method: 'DELETE' });
}

export async function deleteWaAccount(account: WAAccount | string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const resp = await api<DeleteWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}`, { method: 'DELETE' });
  if (!resp.success || resp.error?.message) throw new Error(resp.error?.message || 'delete WAAccount failed');
  return resp;
}

export const probeWaPhoneSMS = (input: WaPhoneInput) => api<WaWorkflowResponse>('/api/wa/phone/sms-probe', { method: 'POST', body: JSON.stringify(input) });
export const registerWaPhone = (input: WaPhoneInput) => api<WaWorkflowResponse>('/api/wa/register', { method: 'POST', body: JSON.stringify(input) });
export const checkWaLoginState = (input: { login_state_id?: string; registered_identity_id?: string; wa_account_id?: string; client_profile_id?: string; remote_timeout_seconds?: number }) => api<WaWorkflowResponse>('/api/wa/login-state-check', { method: 'POST', body: JSON.stringify(input) });

export async function getWaTwoFactorAuthStatus(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return requireAccountSettingsResponse(await api<GetTwoFactorAuthStatusResponse>(`/api/wa/account-settings/2fa/status?${new URLSearchParams({ wa_account_id: accountID })}`));
}

export function submitWaRegistrationOTP(account: WAAccount | string, otp: string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  return api<WaWorkflowResponse>('/api/wa/actions/registration/resume-otp', { method: 'POST', body: JSON.stringify({ wa_account_id: accountID, otp }) });
}

export async function setWaTwoFactorAuthSettings(account: WAAccount, pin: string) {
  return requireAccountSettingsResponse(await api<SetTwoFactorAuthSettingsResponse>('/api/wa/account-settings/2fa', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), pin }) }));
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
export const waAccountTitle = (account?: WAAccount) => account?.phone?.e164_number || waAccountID(account) || '-';
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

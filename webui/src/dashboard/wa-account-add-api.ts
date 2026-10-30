import { v4 as uuidv4 } from 'uuid';
import type { AccountAddResponse, BeginAccountAddRequest } from '../proto/byte/v/forge/waapp/v1/account_add';
import type { VerificationDeliveryMethod } from '../proto/byte/v/forge/waapp/v1/registration';
import { api } from './wa-api';

const root = '/api/wa/account-add';
const context = () => ({ request_id: uuidv4() });
const flowPath = (flowID: string) => `${root}/${encodeURIComponent(flowID)}`;

export const beginAccountAdd = (request: Omit<BeginAccountAddRequest, 'context'>) => api<AccountAddResponse>(root, { method: 'POST', body: JSON.stringify({ ...request, context: context() }) });
export const getAccountAdd = (flowID: string, signal?: AbortSignal) => api<AccountAddResponse>(flowPath(flowID), { signal });
export const cancelAccountAdd = (flowID: string) => api<AccountAddResponse>(flowPath(flowID), { method: 'DELETE' });
export const requestAccountAddCode = (flowID: string, method: VerificationDeliveryMethod) => api<AccountAddResponse>(`${flowPath(flowID)}/code`, { method: 'POST', body: JSON.stringify({ context: context(), delivery_method: method }) });
export const submitAccountAddCode = (flowID: string, code: string) => api<AccountAddResponse>(`${flowPath(flowID)}/code/submit`, { method: 'POST', body: JSON.stringify({ context: context(), code }) });
export const submitAccountAddPassword = (flowID: string, password: string) => api<AccountAddResponse>(`${flowPath(flowID)}/password`, { method: 'POST', body: JSON.stringify({ context: context(), password: { value: password } }) });
export const deliverAccountAdd = (flowID: string) => api<AccountAddResponse>(`${flowPath(flowID)}/deliver`, { method: 'POST', body: JSON.stringify({ context: context() }) });

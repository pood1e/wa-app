import { v4 as uuidv4 } from 'uuid';
import type { LinkDeviceResponse, ListLinkedDevicesResponse, UnlinkDeviceResponse } from '../proto/byte/v/forge/waapp/v1/linked_devices';
import { api, getWaResponse, mutateWaResponse } from './wa-api';

const root = '/api/wa/linked-devices';

export function listWaLinkedDevices(accountID: string, signal?: AbortSignal) {
  return getWaResponse<ListLinkedDevicesResponse>(`${root}?${new URLSearchParams({ wa_account_id: accountID })}`, signal).then((response) => ({ ...response, devices: response.devices ?? [] }));
}

export function linkWaDevice(accountID: string, qr: string) {
  return api<LinkDeviceResponse>(root, { method: 'POST', body: JSON.stringify({ context: { request_id: uuidv4() }, wa_account_id: accountID, qr_code: { value: qr } }) });
}

export function unlinkWaDevice(accountID: string, ref: string) {
  return mutateWaResponse<UnlinkDeviceResponse>(`${root}/unlink`, { method: 'POST', body: JSON.stringify({ context: { request_id: uuidv4() }, wa_account_id: accountID, device_ref: ref }) });
}

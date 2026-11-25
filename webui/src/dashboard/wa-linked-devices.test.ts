import { afterEach, describe, expect, it, vi } from 'vitest';
import { linkWaDevice, listWaLinkedDevices, unlinkWaDevice } from './wa-linked-devices-api';

afterEach(() => { vi.unstubAllGlobals(); });

describe('linked-device requests', () => {
  it('normalizes an empty protobuf device list', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}')));
    await expect(listWaLinkedDevices('account')).resolves.toEqual({ devices: [] });
  });

  it('submits the authorization once in the sensitive request body', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ device: { device_ref: '100001:2@lid' } })));
    vi.stubGlobal('fetch', fetch);
    await linkWaDevice('account', 'synthetic-qr');
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/wa/linked-devices');
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({ wa_account_id: 'account', qr_code: { value: 'synthetic-qr' } });
    expect(fetch.mock.calls[0][1].method).toBe('POST');
  });

  it('preserves an uncertain result and a confirmed device with a checkpoint error', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ outcome_unknown: true, error: { message: 'interrupted' } })));
    vi.stubGlobal('fetch', fetch);
    await expect(linkWaDevice('account', 'synthetic-qr')).resolves.toMatchObject({ outcome_unknown: true });
    const response = { device: { device_ref: '100001:2@lid' }, error: { message: 'checkpoint unavailable' } };
    fetch.mockResolvedValue(new Response(JSON.stringify(response)));
    await expect(linkWaDevice('account', 'synthetic-other-qr')).resolves.toEqual(response);
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('does not resubmit a disconnected authorization', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    vi.stubGlobal('fetch', fetch);
    await expect(linkWaDevice('account', 'synthetic-qr')).rejects.toThrow('无法连接服务');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('binds revocation to the selected account and companion', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{}'));
    vi.stubGlobal('fetch', fetch);
    await unlinkWaDevice('account', '100001:2@lid');
    expect(fetch.mock.calls[0][0]).toBe('/api/wa/linked-devices/unlink');
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({ wa_account_id: 'account', device_ref: '100001:2@lid' });
  });
});

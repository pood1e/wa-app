import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, getWaMessages, resolveWaContacts } from './wa-api';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function untilAborted(signal?: AbortSignal | null): Promise<never> {
  if (!signal) throw new Error('request is missing a cancellation signal');
  return new Promise((_resolve, reject) => {
    if (signal.aborted) reject(signal.reason);
    else signal.addEventListener('abort', () => reject(signal.reason), { once: true });
  });
}

describe('dashboard HTTP requests', () => {
  it('returns JSON with same-origin credentials', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ accounts: [] })));
    vi.stubGlobal('fetch', fetch);
    await expect(api('/api/wa/accounts')).resolves.toEqual({ accounts: [] });
    expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: 'same-origin' });
    expect(fetch.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal);
  });

  it('ends a suspended request with a readable timeout', async () => {
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => untilAborted(init?.signal)));
    const response = api('/api/wa/accounts');
    deadline.abort(new DOMException('request timed out', 'TimeoutError'));
    await expect(response).rejects.toThrow('连接服务超时');
  });

  it('keeps the deadline active while reading the response body', async () => {
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    const stream = new ReadableStream({
      start(controller) {
        deadline.signal.addEventListener('abort', () => controller.error(deadline.signal.reason));
      },
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(stream)));
    const response = api('/api/wa/accounts');
    await Promise.resolve();
    deadline.abort(new DOMException('body timed out', 'TimeoutError'));
    await expect(response).rejects.toThrow('连接服务超时');
  });

  it('preserves caller cancellation', async () => {
    const caller = new AbortController();
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => untilAborted(init?.signal)));
    const response = api('/api/wa/accounts', { signal: caller.signal });
    const reason = new DOMException('view closed', 'AbortError');
    caller.abort(reason);
    await expect(response).rejects.toBe(reason);
  });

  it('cancels a message request when its conversation is left', async () => {
    const conversation = new AbortController();
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => untilAborted(init?.signal)));
    const response = getWaMessages('account_fixture', 'contact_fixture', '', conversation.signal);
    const reason = new DOMException('conversation changed', 'AbortError');
    conversation.abort(reason);
    await expect(response).rejects.toBe(reason);
  });

  it('cancels background contact resolution when its view is left', async () => {
    const view = new AbortController();
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => untilAborted(init?.signal)));
    const response = resolveWaContacts('account_fixture', ['100001@lid'], view.signal);
    const reason = new DOMException('view changed', 'AbortError');
    view.abort(reason);
    await expect(response).rejects.toBe(reason);
  });

  it('shows connection failures without retrying a submitted operation', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    vi.stubGlobal('fetch', fetch);
    await expect(api('/api/wa/account-add', { method: 'POST', body: '{}' })).rejects.toThrow('无法连接服务');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('preserves HTTP errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })));
    await expect(api('/api/wa/accounts')).rejects.toThrow('HTTP 503');
  });
});

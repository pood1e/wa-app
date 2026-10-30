import { afterEach, describe, expect, it, vi } from 'vitest';
import { AvatarRequestError, avatarRecoveryInterval, avatarRetryDelay, loadWaAvatar, retryWaAvatar } from './wa-avatar-loader';

afterEach(() => vi.unstubAllGlobals());

describe('avatar responses', () => {
  it('returns image bytes', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('fixture-image', { headers: { 'Content-Type': 'image/jpeg' } }));
    vi.stubGlobal('fetch', fetch);
    const blob = await loadWaAvatar('/fixture', new AbortController().signal);
    expect(await blob?.text()).toBe('fixture-image');
    expect(fetch.mock.calls[0][1].cache).toBeUndefined();
  });

  it.each([404, 410])('treats HTTP %s as a missing picture', async (status) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status })));
    expect(await loadWaAvatar('/fixture', new AbortController().signal)).toBeNull();
  });

  it.each([429, 502, 503, 504])('retries HTTP %s without caching a missing picture', async (status) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status, headers: { 'Retry-After': '5' } })));
    const error = await loadWaAvatar('/fixture', new AbortController().signal).catch((error: Error) => error);
    expect(error).toBeInstanceOf(AvatarRequestError);
    expect(retryWaAvatar(0, error as Error)).toBe(true);
    expect(avatarRetryDelay(0, error as Error)).toBe(5000);
    expect(retryWaAvatar(3, error as Error)).toBe(false);
  });

  it.each([401, 403])('does not retry HTTP %s', async (status) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status })));
    const error = await loadWaAvatar('/fixture', new AbortController().signal).catch((error: Error) => error);
    expect(retryWaAvatar(0, error as Error)).toBe(false);
  });

  it('rejects successful HTML responses instead of treating them as a missing picture', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<html>proxy response</html>', { headers: { 'Content-Type': 'text/html' } })));
    await expect(loadWaAvatar('/fixture', new AbortController().signal)).rejects.toBeInstanceOf(AvatarRequestError);
  });

  it('retries network and timeout failures with a finite backoff', () => {
    expect(retryWaAvatar(0, new TypeError('network error'))).toBe(true);
    expect(retryWaAvatar(0, new DOMException('timed out', 'TimeoutError'))).toBe(true);
    expect(retryWaAvatar(0, new DOMException('cancelled', 'AbortError'))).toBe(false);
    expect(avatarRetryDelay(1, new TypeError('network error'))).toBe(2000);
  });

  it('recovers exhausted transient failures with a bounded interval', () => {
    const state = { data: undefined, error: new AvatarRequestError(503, 0), errorUpdateCount: 1 };
    expect(avatarRecoveryInterval(state)).toBe(30000);
    expect(avatarRecoveryInterval({ ...state, errorUpdateCount: 2 })).toBe(30000);
    expect(avatarRecoveryInterval({ ...state, errorUpdateCount: 3 })).toBe(false);
  });

  it('keeps cached pictures, known absence and permanent errors out of recovery polling', () => {
    const state = { data: undefined, error: new AvatarRequestError(503, 0), errorUpdateCount: 1 };
    expect(avatarRecoveryInterval({ ...state, data: new Blob(['cached']) })).toBe(false);
    expect(avatarRecoveryInterval({ ...state, data: null, error: null })).toBe(false);
    expect(avatarRecoveryInterval({ ...state, error: new AvatarRequestError(403, 0) })).toBe(false);
    expect(avatarRecoveryInterval({ ...state, error: new DOMException('cancelled', 'AbortError') })).toBe(false);
  });
});

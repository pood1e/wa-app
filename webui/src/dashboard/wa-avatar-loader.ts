import type { QueryState } from '@tanstack/react-query';

export class AvatarRequestError extends Error {
  constructor(readonly status: number, readonly retryAfter: number) {
    super(`Avatar request failed: HTTP ${status}`);
  }
}

export async function loadWaAvatar(src: string, signal: AbortSignal): Promise<Blob | null> {
  const response = await fetch(src, { credentials: 'same-origin', signal: AbortSignal.any([signal, AbortSignal.timeout(60000)]) });
  if (response.status === 404 || response.status === 410) return null;
  if (!response.ok) throw new AvatarRequestError(response.status, retryAfterMilliseconds(response.headers.get('Retry-After')));
  if (!response.headers.get('content-type')?.startsWith('image/')) throw new AvatarRequestError(200, 0);
  return response.blob();
}

export function retryWaAvatar(failures: number, error: Error) {
  if (failures >= 3) return false;
  if (error instanceof AvatarRequestError) return error.status === 429 || error.status >= 500;
  return error instanceof TypeError || error.name === 'TimeoutError';
}

export function avatarRetryDelay(attempt: number, error: Error) {
  if (error instanceof AvatarRequestError && error.retryAfter > 0) return Math.min(error.retryAfter, 30000);
  return Math.min(1000 * 2 ** attempt, 5000);
}

export function avatarRecoveryInterval(state: Pick<QueryState<Blob | null>, 'data' | 'error' | 'errorUpdateCount'>): number | false {
  if (state.data || !state.error || !retryWaAvatar(0, state.error) || state.errorUpdateCount >= 3) return false;
  return 30000;
}

function retryAfterMilliseconds(value: string | null) {
  if (!value) return 0;
  const seconds = Number(value);
  return Number.isFinite(seconds) ? Math.max(0, seconds * 1000) : Math.max(0, Date.parse(value) - Date.now()) || 0;
}

export function remainingSeconds(duration: string | undefined, observed: string | undefined, now: number) {
  const seconds = Number.parseFloat(duration || '0');
  if (!Number.isFinite(seconds) || seconds <= 0) return 0;
  const at = Date.parse(observed || '');
  return Math.max(0, Math.ceil(seconds - (Number.isFinite(at) ? (now - at) / 1000 : 0)));
}

import { useCallback, useEffect, useRef } from 'react';

export function useWaRequestSignal() {
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  return useCallback(() => {
    if (!request.current || request.current.signal.aborted) request.current = new AbortController();
    return request.current.signal;
  }, []);
}

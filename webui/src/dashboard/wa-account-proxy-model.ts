import type { AccountProxy } from '../proto/byte/v/forge/waapp/v1/account_proxy';

export function validAccountProxy(value: AccountProxy | undefined) {
  return !value || Boolean(value.host.trim() && Number.isInteger(value.port) && value.port > 0 && value.port <= 65535 && (!value.password?.value || value.username));
}

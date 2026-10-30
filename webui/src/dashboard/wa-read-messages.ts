import { useEffect, useRef, useSyncExternalStore } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { AccountMessage } from '../proto/byte/v/forge/waapp/v1/messaging';
import { AccountMessageDirection, MessageEncryptionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WaError } from '../proto/byte/v/forge/waapp/v1/common';
import { markWaMessagesRead, waKeys } from './wa-api';

class ReadReceiptError extends Error {
  constructor(readonly detail: WaError) {
    super(detail.retryable ? '已读已保存，回执等待重试' : `已读已保存，回执发送失败：${detail.message}`);
  }
}

function subscribeVisibility(onChange: () => void) {
  document.addEventListener('visibilitychange', onChange);
  window.addEventListener('focus', onChange);
  window.addEventListener('blur', onChange);
  return () => { document.removeEventListener('visibilitychange', onChange); window.removeEventListener('focus', onChange); window.removeEventListener('blur', onChange); };
}

export function useWaReadMessages(accountID: string, contactID: string, messages: AccountMessage[], localOnly: boolean, updatedAt: number) {
  const visible = useSyncExternalStore(subscribeVisibility, () => document.visibilityState === 'visible' && document.hasFocus());
  const attempted = useRef({ key: '', at: 0 });
  const client = useQueryClient();
  const mutation = useMutation({
    mutationFn: async (input: { accountID: string; ids: string[]; contactID: string; localOnly: boolean }) => {
      const response = await markWaMessagesRead(input.accountID, { accountMessageIds: input.ids, localOnly: input.localOnly });
      if (response.error?.message) throw new Error(response.error.message);
      if (response.receipt_error) throw new ReadReceiptError(response.receipt_error);
      return response;
    },
    retry: (count, error) => count < 2 && error instanceof ReadReceiptError && error.detail.retryable,
    retryDelay: (attempt) => 1000 * 2 ** attempt,
    onSettled: (_data, _error, input) => Promise.all([client.invalidateQueries({ queryKey: waKeys.messages(input.accountID, input.contactID) }), client.invalidateQueries({ queryKey: waKeys.contacts(input.accountID) })]),
  });
  const ids = messages.filter((message) => message.direction === AccountMessageDirection.ACCOUNT_MESSAGE_DIRECTION_INBOUND && (message.encryption_state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_DECRYPTED || message.encryption_state === MessageEncryptionState.MESSAGE_ENCRYPTION_STATE_PLAINTEXT) && (!message.read_at || (!localOnly && message.provider_message_id && !message.read_receipt_sent_at))).map((message) => message.account_message_id || '').filter(Boolean).sort().join(',');
  const { mutate, isPending } = mutation;
  useEffect(() => {
    if (!visible || !contactID || !ids || isPending) return;
    const key = `${accountID}/${contactID}/${ids}/${localOnly}`;
    if (attempted.current.key === key && Date.now() - attempted.current.at < 30_000) return;
    attempted.current = { key, at: Date.now() };
    mutate({ accountID, ids: ids.split(','), contactID, localOnly });
  }, [accountID, contactID, ids, localOnly, visible, isPending, mutate, updatedAt]);
  const current = mutation.variables?.accountID === accountID && mutation.variables.contactID === contactID && mutation.variables.localOnly === localOnly;
  return { ...mutation, error: current && ids ? mutation.error : null };
}

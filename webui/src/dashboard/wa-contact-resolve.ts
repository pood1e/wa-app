import { useMemo } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { WAContact as WAContactRecord } from '../proto/byte/v/forge/waapp/v1/contacts';
import { resolveWaContacts, waKeys } from './wa-api';

const MAX_AUTO_RESOLVE_CONTACTS = 20;
const AUTO_RESOLVE_CACHE_TIME = 5 * 60_000;

export function useWaContactAutoResolve(accountID: string, records: WAContactRecord[]) {
  const queryClient = useQueryClient();
  const targets = useMemo(() => unresolvedContactJIDs(records).sort().slice(0, MAX_AUTO_RESOLVE_CONTACTS), [records]);
  useQuery({
    queryKey: [...waKeys.contactResolve(accountID), targets],
    enabled: (query) => Boolean(accountID && targets.length) && (!query.state.error || Date.now() - query.state.errorUpdatedAt >= AUTO_RESOLVE_CACHE_TIME),
    staleTime: AUTO_RESOLVE_CACHE_TIME,
    gcTime: AUTO_RESOLVE_CACHE_TIME,
    retry: false,
    queryFn: async ({ signal }) => {
      const result = await resolveWaContacts(accountID, targets, signal);
      await queryClient.invalidateQueries({ queryKey: waKeys.contacts(accountID) });
      return result;
    },
  });
}

function unresolvedContactJIDs(records: WAContactRecord[]) {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const record of records) {
    const jid = (record.jid || '').trim();
    if (!jid.endsWith('@lid') || seen.has(jid) || !needsResolve(record)) continue;
    seen.add(jid);
    out.push(jid);
  }
  return out;
}

function needsResolve(record: WAContactRecord) {
  const name = (record.display_name || '').trim();
  return !record.profile_picture_id || !record.number || !name || name === '未知联系人' || name.startsWith('LID ') || name.startsWith('联系人 ');
}

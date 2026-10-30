import type { WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';

export function canonicalContactRef(value: string) { return value.trim().replace(/^\+/, '').replace(/:\d+(?=@)/, ''); }

export function contactAliasIndex(records: WAContact[]) {
  const preferred = new Map<string, WAContact>();
  for (const record of records) {
    if (!record.number) continue;
    if (!preferred.has(record.number) || record.jid?.endsWith('@lid')) preferred.set(record.number, record);
  }
  const aliases = new Map<string, string>();
  for (const record of records) {
    const target = preferred.get(record.number) || record;
    const id = canonicalContactRef(target.jid || target.number || target.contact_id || '');
    for (const ref of [record.jid, record.number, record.contact_id, record.number ? `${record.number}@s.whatsapp.net` : '']) {
      if (ref) aliases.set(canonicalContactRef(ref), id);
    }
  }
  return aliases;
}

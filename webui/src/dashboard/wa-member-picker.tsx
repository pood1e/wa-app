import { useState } from 'react';
import { Search } from 'lucide-react';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { WaContact } from './wa-chat-model';
import { WaContactAvatar } from './wa-contact-avatar';
import { canonicalContactRef } from './wa-contact-aliases';
import { Input } from '@/components/ui/input';

export function WaMemberPicker({ contacts, selected, onChange, exclude = [], disabled }: { contacts: WaContact[]; selected: string[]; onChange: (refs: string[]) => void; exclude?: string[]; disabled?: boolean }) {
  const [search, setSearch] = useState('');
  const excluded = new Set(exclude.map(canonicalContactRef));
  const available = contacts.filter((contact) => contact.kind !== WAContactKind.WA_CONTACT_KIND_GROUP && (contact.id.endsWith('@lid') || contact.id.endsWith('@s.whatsapp.net')) && !excluded.has(canonicalContactRef(contact.id)) && `${contact.title} ${contact.subtitle}`.toLowerCase().includes(search.trim().toLowerCase()));
  return (
    <fieldset className="grid gap-2" disabled={disabled}>
      <legend className="mb-2 text-sm font-medium">选择成员{selected.length > 0 ? ` · ${selected.length}` : ''}</legend>
      <div className="relative"><Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" /><Input className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} aria-label="搜索群成员" placeholder="搜索联系人" /></div>
      <div className="max-h-64 overflow-y-auto rounded-xl border p-1">
        {available.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted-foreground">没有匹配的联系人，可先按手机号添加联系人。</p>}
        {available.map((contact) => <label key={contact.id} className="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 hover:bg-muted"><input type="checkbox" className="size-4 accent-primary" checked={selected.includes(contact.id)} onChange={(event) => onChange(event.target.checked ? [...selected, contact.id] : selected.filter((ref) => ref !== contact.id))} /><WaContactAvatar size="xs" contact={contact} /><span className="min-w-0"><span className="block truncate text-sm">{contact.title}</span><span className="block truncate text-xs text-muted-foreground">{contact.subtitle}</span></span></label>)}
      </div>
    </fieldset>
  );
}

import type { MouseEvent } from 'react';
import { useMemo, useRef, useState } from 'react';
import { CircleUserRound, Loader2, MessageCircle, Search, Trash2, X } from 'lucide-react';
import { Link, NavLink } from 'react-router';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { WaContact } from './wa-chat-model';
import { formatChatTime } from './wa-chat-model';
import { WaContactAvatar } from './wa-contact-avatar';
import { WaContactOtpBanner } from './wa-contact-otp-banner';
import { waAccountPath, waContactPath } from './wa-route-paths';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { SidebarTrigger } from '@/components/ui/sidebar';
import { WaNewContact } from './wa-new-contact';
import { WaGroupMenu } from './wa-group-menu';

type ChatFilter = 'all' | 'unread' | 'groups';
const filters: Array<{ value: ChatFilter; label: string }> = [{ value: 'all', label: '全部' }, { value: 'unread', label: '未读' }, { value: 'groups', label: '群组' }];

export function WaContactList({ accountID, selfRef, contacts, selectedID, loading, error, deletingID, otp, readOnly, onDeleteContact }: { accountID: string; selfRef: string; contacts: WaContact[]; selectedID: string; loading: boolean; error?: string; deletingID?: string; otp?: OtpMessage; readOnly?: boolean; onDeleteContact: (contactID: string) => void }) {
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<ChatFilter>('all');
  const visibleContacts = useMemo(() => filterContacts(contacts, query, filter), [contacts, query, filter]);
  const unreadCount = contacts.reduce((sum, contact) => sum + contact.unreadCount, 0);
  return (
    <aside aria-label="聊天列表" className="grid h-full min-h-0 grid-rows-[auto_1fr] overflow-hidden border-r border-border bg-card">
      <div className="pb-2">
        <header className="flex h-[76px] items-center justify-between gap-3 px-5">
          <div className="flex items-center gap-3">
            <SidebarTrigger className="size-9 rounded-full md:hidden" aria-label="切换账号" title="切换账号" />
            <h1 className="text-[23px] font-semibold tracking-tight">聊天</h1>
            {loading ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
          </div>
          <div className="flex items-center gap-1"><WaNewContact accountID={accountID} disabled={readOnly} /><WaGroupMenu accountID={accountID} selfRef={selfRef} contacts={contacts} disabled={readOnly} /><Button asChild size="icon" variant="ghost" className="size-10 rounded-full text-muted-foreground" title="账号详情" aria-label="账号详情"><Link to={waAccountPath(accountID)}><CircleUserRound className="size-5" /></Link></Button></div>
        </header>
        <div className="px-4">
          <div className="relative">
            <Search className="pointer-events-none absolute left-4 top-1/2 size-[18px] -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input className="h-10 rounded-full border-transparent bg-muted pl-11 pr-10 shadow-none focus-visible:ring-2 dark:bg-muted" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索联系人或聊天" aria-label="搜索联系人" />
            {query ? <Button type="button" variant="ghost" size="icon-sm" className="absolute right-2 top-1/2 -translate-y-1/2 rounded-full text-muted-foreground" aria-label="清除搜索" onClick={() => setQuery('')}><X /></Button> : null}
          </div>
          <div role="group" aria-label="筛选聊天" className="flex gap-2 py-3">
            {filters.map((item) => <button key={item.value} type="button" aria-pressed={filter === item.value} className={`inline-flex h-8 items-center gap-2 rounded-full px-3.5 text-[13px] transition-colors focus-visible:outline-2 focus-visible:outline-ring ${filter === item.value ? 'bg-accent text-accent-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'}`} onClick={() => setFilter(item.value)}>{item.label}{item.value === 'unread' && unreadCount > 0 ? <span className="text-[11px]" aria-hidden="true">{unreadCount}</span> : null}</button>)}
          </div>
          <WaContactOtpBanner otp={otp} />
        </div>
      </div>
      <div className="min-h-0 overflow-y-auto px-2 pb-2">
        {error && <Alert className="mb-2" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {!loading && !error && contacts.length === 0 && <ContactEmpty title="暂无联系人" description="收到消息后会显示在这里。" />}
        {!loading && !error && contacts.length > 0 && visibleContacts.length === 0 && <ContactEmpty title={filter === 'unread' && !query ? '没有未读聊天' : filter === 'groups' && !query ? '暂无群聊' : '没有匹配的聊天'} description={query ? '试试其他名字或手机号。' : '新的聊天会显示在这里。'} />}
        {visibleContacts.map((contact) => <ContactRow key={contact.id} accountID={accountID} contact={contact} selected={contact.id === selectedID} deleting={deletingID === contact.id} onDeleteContact={onDeleteContact} />)}
      </div>
    </aside>
  );
}

function ContactEmpty({ title, description }: { title: string; description: string }) {
  return <Empty className="border-0 px-6 py-12"><EmptyHeader><MessageCircle className="mx-auto mb-2 size-8 text-muted-foreground/50" strokeWidth={1.5} /><EmptyTitle className="text-sm">{title}</EmptyTitle><EmptyDescription>{description}</EmptyDescription></EmptyHeader></Empty>;
}

function ContactRow({ accountID, contact, selected, deleting, onDeleteContact }: { accountID: string; contact: WaContact; selected: boolean; deleting: boolean; onDeleteContact: (contactID: string) => void }) {
  const unread = contact.unreadCount > 0;
  const holdTimer = useRef<number | undefined>(undefined);
  const revealedByHold = useRef(false);
  const [deleteVisible, setDeleteVisible] = useState(false);
  const revealDelete = (blockNextClick = true) => {
    revealedByHold.current = blockNextClick;
    setDeleteVisible(true);
  };
  const clearHold = () => window.clearTimeout(holdTimer.current);
  const startHold = () => {
    clearHold();
    holdTimer.current = window.setTimeout(() => revealDelete(), 650);
  };
  const openOrReveal = (event: MouseEvent<HTMLAnchorElement>) => {
    if (revealedByHold.current) {
      event.preventDefault();
      revealedByHold.current = false;
      return;
    }
  };
  return (
    <div className={`group relative rounded-lg transition-colors ${selected ? 'bg-muted' : 'hover:bg-muted/60'}`} onContextMenu={(event) => { event.preventDefault(); revealDelete(false); }}>
      <NavLink className="grid min-h-[76px] min-w-0 grid-cols-[48px_minmax(0,1fr)] items-center gap-3 px-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" to={waContactPath(accountID, contact.id)} aria-current={selected ? 'page' : undefined} onClick={openOrReveal} onPointerDown={startHold} onPointerLeave={clearHold} onPointerCancel={clearHold} onPointerUp={clearHold}>
        <WaContactAvatar contact={contact} />
        <span className="grid min-w-0 gap-1.5 border-b border-border/60 py-4 group-last:border-transparent">
          <span className="flex min-w-0 items-center justify-between gap-3">
            <span className={`truncate text-[15px] ${unread ? 'font-semibold' : 'font-medium'}`}>{contact.title}</span>
            <time className={`shrink-0 text-[11px] ${unread ? 'font-medium text-primary' : 'text-muted-foreground'}`}>{formatChatTime(contact.lastAt)}</time>
          </span>
          <span className="flex min-w-0 items-center justify-between gap-3 pr-6">
            <span className={`truncate text-[13px] ${unread ? 'text-foreground' : 'text-muted-foreground'}`}>{contact.preview || contact.subtitle || '点击开始聊天'}</span>
            {unread ? <span className="inline-flex min-w-5 shrink-0 items-center justify-center rounded-full bg-primary px-1.5 py-0.5 text-[11px] font-semibold text-primary-foreground" aria-label={`${contact.unreadCount} 条未读`}>{contact.unreadCount}</span> : null}
          </span>
        </span>
      </NavLink>
      <Button className={`absolute bottom-2.5 right-2 size-7 rounded-full text-muted-foreground hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 ${deleteVisible ? 'opacity-100' : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'}`} variant="ghost" size="icon" type="button" title="删除联系人" aria-label={`删除 ${contact.title} 的聊天`} disabled={deleting} onClick={() => onDeleteContact(contact.id)}>{deleting ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 size={14} />}</Button>
    </div>
  );
}

function filterContacts(contacts: WaContact[], query: string, filter: ChatFilter) {
  const needle = query.trim().toLowerCase();
  return contacts.filter((contact) => (filter !== 'unread' || contact.unreadCount > 0) && (filter !== 'groups' || contact.kind === WAContactKind.WA_CONTACT_KIND_GROUP) && (!needle || `${contact.title} ${contact.subtitle} ${contact.preview} ${contact.id}`.toLowerCase().includes(needle)));
}

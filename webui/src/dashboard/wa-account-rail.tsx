import { useMemo, useState } from 'react';
import { Info, Loader2, Plus } from 'lucide-react';
import { Link, NavLink } from 'react-router';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInput,
  SidebarMenuAction,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID } from './wa-api';
import { WaAccountAvatar } from './wa-account-avatar';
import { WaConnectionDot } from './wa-connection-dot';
import { WaThemeToggle } from './wa-theme-toggle';
import { waAccountPath, waChatsPath } from './wa-route-paths';

type RailProps = { accounts: WAAccount[]; selectedID: string; avatarVersion: string; connections: Map<string, LongConnectionState>; loading: boolean; connectionsLoading: boolean; hasNextPage: boolean; loadingMore: boolean; onLoadMore: () => void };
type AccountItemProps = { account: WAAccount; selected: boolean; avatarVersion: string; connection?: LongConnectionState; loading: boolean };

const railButtonClass = 'h-12 gap-2 p-1! group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:size-12! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-1!';
const accountActionClass = 'top-1/2! right-2! size-7! -translate-y-1/2 rounded-lg';
const collapsedTextClass = 'group-data-[collapsible=icon]:hidden';

export function WaAccountRail({ accounts, selectedID, avatarVersion, connections, loading, connectionsLoading, hasNextPage, loadingMore, onLoadMore }: RailProps) {
  const [query, setQuery] = useState('');
  const { state } = useSidebar();
  const expanded = state === 'expanded';
  const visibleAccounts = useFilteredAccounts(accounts, expanded ? query : '');
  return (
    <Sidebar collapsible="icon" aria-label="WA 账号" className="border-r border-border">
      <SidebarHeader className="h-16 justify-center border-b border-sidebar-border">
        <RailHeader value={query} onChange={setQuery} />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup className="p-1">
          <SidebarGroupContent>
            <SidebarMenu>
              {loading ? <LoadingItems /> : null}
              {visibleAccounts.map((account) => {
                const id = waAccountID(account);
                return <AccountItem key={id} account={account} selected={id === selectedID} avatarVersion={avatarVersion} connection={connections.get(id)} loading={connectionsLoading} />;
              })}
            </SidebarMenu>
            {!loading && visibleAccounts.length === 0 ? <EmptyAccounts searching={query.trim() !== ''} /> : null}
            {expanded && hasNextPage ? <LoadMoreButton loading={loadingMore} onLoadMore={onLoadMore} /> : null}
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="border-t border-sidebar-border p-1">
        <WaThemeToggle className="size-8 group-data-[collapsible=icon]:mx-auto" />
      </SidebarFooter>
      <SidebarRail aria-label={expanded ? '收起账号栏' : '展开账号栏'} title={expanded ? '收起账号栏' : '展开账号栏'} />
    </Sidebar>
  );
}

function RailHeader({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="flex h-10 items-center gap-2 group-data-[collapsible=icon]:justify-center">
      <SidebarInput className="h-8 group-data-[collapsible=icon]:hidden" value={value} onChange={(event) => onChange(event.target.value)} placeholder="搜索手机号" aria-label="搜索账号" />
      <Button asChild size="icon" variant="ghost" className="size-8 group-data-[collapsible=icon]:hidden" title="添加账号" aria-label="添加账号"><Link to="/accounts/new"><Plus size={16} /></Link></Button>
      <SidebarTrigger className="shrink-0" aria-label="切换账号栏" title="切换账号栏" />
    </div>
  );
}

function AccountItem({ account, selected, avatarVersion, connection, loading }: AccountItemProps) {
  const id = waAccountID(account);
  const label = waAccountRailLabel(account);
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild size="lg" isActive={selected} tooltip={label.tooltip} className={railButtonClass}>
        <NavLink to={waChatsPath(id)} title={label.tooltip} aria-label={label.tooltip}>
          <span className="relative shrink-0">
            <WaAccountAvatar account={account} version={avatarVersion} size="md" />
            <WaConnectionDot className="absolute bottom-0 right-0" connection={connection} loading={loading} />
          </span>
          <span className={`min-w-0 flex-1 ${collapsedTextClass}`}>
            <span className="block truncate text-sm font-medium">{label.primary}</span>
            {label.subtitle ? <span className="block truncate font-mono text-xs text-muted-foreground">{label.subtitle}</span> : null}
          </span>
        </NavLink>
      </SidebarMenuButton>
      <SidebarMenuAction asChild showOnHover={!selected} className={accountActionClass}>
        <Link to={waAccountPath(id)} title="账号详情" aria-label={`${label.tooltip} 账号详情`}><Info /></Link>
      </SidebarMenuAction>
    </SidebarMenuItem>
  );
}

function LoadMoreButton({ loading, onLoadMore }: { loading: boolean; onLoadMore: () => void }) {
  return <Button className="mt-2 w-full" variant="outline" onClick={onLoadMore} disabled={loading}>{loading ? <Loader2 className="size-4 animate-spin" /> : null}加载更多账号</Button>;
}

function LoadingItems() {
  return <><SidebarMenuItem><SidebarMenuSkeleton showIcon /></SidebarMenuItem><SidebarMenuItem><SidebarMenuSkeleton showIcon /></SidebarMenuItem></>;
}

function EmptyAccounts({ searching }: { searching: boolean }) {
  return <Empty className="mt-4 border-0 p-4"><EmptyHeader><EmptyTitle>{searching ? '没有匹配账号' : '还没有账号'}</EmptyTitle><EmptyDescription>{searching ? '没有匹配的已加载账号' : '添加账号后会显示在这里'}</EmptyDescription></EmptyHeader></Empty>;
}

function useFilteredAccounts(accounts: WAAccount[], query: string) {
  return useMemo(() => {
    const normalized = normalizeQuery(query);
    if (!normalized) return accounts;
    return accounts.filter((account) => normalizeQuery(waAccountPhoneSearchText(account)).includes(normalized));
  }, [accounts, query]);
}

function waAccountPhone(account: WAAccount) {
  const phone = account.phone;
  const callingCode = phoneCallingCode(phone?.country_calling_code || '');
  const nationalNumber = (phone?.national_number || '').trim();
  if (callingCode && nationalNumber) return `${callingCode} ${nationalNumber}`;
  return formatE164Number(phone?.e164_number || '', callingCode);
}

function waAccountPhoneSearchText(account: WAAccount) {
  const phone = account.phone;
  return `${waAccountPhone(account)} ${phone?.e164_number || ''} ${phone?.national_number || ''}`;
}

function formatE164Number(value: string, callingCode: string) {
  value = value.trim();
  if (!value || !callingCode || !value.startsWith(callingCode)) return value;
  const nationalNumber = value.slice(callingCode.length).trim();
  return nationalNumber ? `${callingCode} ${nationalNumber}` : value;
}

function phoneCallingCode(value: string) {
  value = value.trim();
  if (!value) return '';
  return value.startsWith('+') ? value : `+${value}`;
}

function waAccountRailLabel(account: WAAccount) {
  const name = account.display_name?.trim() || '';
  const phone = waAccountPhone(account);
  const primary = name || phone || '未录入手机号';
  const subtitle = name && phone ? phone : '';
  return { primary, subtitle, tooltip: subtitle ? `${primary} · ${subtitle}` : primary };
}

function normalizeQuery(value: string) {
  return value.trim().toLowerCase().replace(/\s+/g, '');
}

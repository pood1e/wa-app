import { useMemo, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Navigate, Outlet, useMatches, useNavigate, useOutletContext, useParams } from 'react-router';
import { Plus } from 'lucide-react';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { deleteWaAccount, getWaAccounts, getWaClientProfiles, waAccountID, waKeys } from './wa-api';
import { WaAccountAdd } from './wa-account-add';
import { WaAccountInfoPage } from './wa-account-info-page';
import { WaAccountRail } from './wa-account-rail';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaInbox } from './wa-inbox';
import { useWaLongConnectionIndex } from './wa-long-connection-badge';
import { waChatsPath } from './wa-route-paths';
import { Button, LoadingText, SidebarInset, SidebarProvider, ToastMessage, useToastMessage } from './ui';

type WaRouteContext = { accounts: WAAccount[]; accountsLoading: boolean; connections: Map<string, LongConnectionState>; deleting: boolean; refreshAccounts: () => Promise<void>; refreshAccountAvatars: () => void; deleteAccount: (account: WAAccount) => void; done: (message: string) => void; error: (message: string) => void };

const emptyAccounts: WAAccount[] = [];
const accountSidebarStyle = { '--sidebar-width': '15rem', '--sidebar-width-icon': '4rem' } as CSSProperties;

export function WaLayout() {
  const toast = useToastMessage();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [accountAvatarVersion, setAccountAvatarVersion] = useState(() => String(Date.now()));
  const [accountRailExpanded, setAccountRailExpanded] = useState(false);
  const accountsQuery = useInfiniteQuery({ queryKey: waKeys.accounts(), queryFn: ({ pageParam }) => getWaAccounts(pageParam), initialPageParam: '', getNextPageParam: (lastPage) => lastPage.next_cursor || undefined, refetchInterval: 10000 });
  const connections = useWaLongConnectionIndex();
  const accounts = useMemo(() => accountsQuery.data?.pages.flatMap((page) => page.accounts || emptyAccounts) || emptyAccounts, [accountsQuery.data]);
  const selectedID = useSelectedAccountID(accounts);
  const deletion = useMutation({ mutationFn: deleteWaAccount, onSuccess: async () => { toast.showOK('账号已删除'); await refreshAccounts(); navigate('/', { replace: true }); }, onError: toast.showError });
  async function refreshAccounts() {
    await queryClient.invalidateQueries({ queryKey: waKeys.accounts() });
  }
  const refreshAccountAvatars = () => setAccountAvatarVersion(String(Date.now()));
  const context: WaRouteContext = { accounts, accountsLoading: accountsQuery.isLoading, connections: connections.byAccount, deleting: deletion.isPending, refreshAccounts, refreshAccountAvatars, deleteAccount: deletion.mutate, done: toast.showOK, error: toast.showError };
  return (
    <SidebarProvider open={accountRailExpanded} onOpenChange={setAccountRailExpanded} style={accountSidebarStyle} className="h-dvh min-h-0 overflow-hidden bg-background text-foreground">
      <WaAccountRail accounts={accounts} selectedID={selectedID} avatarVersion={accountAvatarVersion} connections={connections.byAccount} loading={accountsQuery.isLoading} connectionsLoading={connections.loading} hasNextPage={Boolean(accountsQuery.hasNextPage)} loadingMore={accountsQuery.isFetchingNextPage} onLoadMore={() => { void accountsQuery.fetchNextPage(); }} />
      <SidebarInset className="h-dvh min-w-0 overflow-hidden"><Outlet context={context} /></SidebarInset>
      <ToastMessage toast={toast.toast} />
    </SidebarProvider>
  );
}

export function WaHomeRoute() {
  const { accounts, accountsLoading } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  const firstID = waAccountID(accounts[0]);
  return firstID ? <Navigate to={waChatsPath(firstID)} replace /> : <NoAccount />;
}

export function WaCreateAccountRoute() {
  const { deleting, refreshAccounts, done, error } = useWaContext();
  return <PageShell title="添加账号"><WaAccountAdd disabled={deleting} onChanged={refreshAccounts} onDone={done} onError={error} /></PageShell>;
}

export function WaAccountInfoRoute() {
  const account = useRouteAccount();
  const { accounts, accountsLoading, deleting, deleteAccount, done, error, refreshAccountAvatars } = useWaContext();
  const accountID = waAccountID(account);
  const profilesQuery = useQuery({ queryKey: waKeys.profiles(accountID), queryFn: () => getWaClientProfiles(accountID), enabled: Boolean(accountID), refetchInterval: 30000 });
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaAccountInfoPage account={account} profiles={profilesQuery.data?.client_profiles || []} profilesLoading={profilesQuery.isLoading} busy={deleting} onDelete={deleteAccount} onDone={done} onError={error} onAvatarChanged={refreshAccountAvatars} />;
}

export function WaInboxRoute() {
  const { contactID = '' } = useParams();
  const account = useRouteAccount();
  const { accounts, accountsLoading } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载消息...</LoadingText></PageCenter>;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaInbox account={account} contactID={contactID} />;
}

function useRouteAccount() {
  const { accountID = '' } = useParams();
  const { accounts } = useWaContext();
  return useMemo(() => accounts.find((account) => waAccountID(account) === accountID), [accounts, accountID]);
}

function useWaContext() {
  return useOutletContext<WaRouteContext>();
}

function useSelectedAccountID(accounts: WAAccount[]) {
  const accountID = [...useMatches()].reverse().find((match) => match.params.accountID)?.params.accountID || '';
  return accounts.some((account) => waAccountID(account) === accountID) ? accountID : '';
}

function AccountFallback({ accounts }: { accounts: WAAccount[] }) {
  const firstID = waAccountID(accounts[0]);
  return firstID ? <Navigate to={waChatsPath(firstID)} replace /> : <NoAccount />;
}

function NoAccount() {
  const navigate = useNavigate();
  return <PageCenter><div className="grid max-w-xs gap-3 text-center"><WhatsAppIcon className="mx-auto size-12" /><div><p className="font-semibold">还没有账号</p><p className="mt-1 text-sm text-muted-foreground">添加账号后即可查看联系人和消息。</p></div><Button onClick={() => navigate('/accounts/new')}><Plus size={16} />添加账号</Button></div></PageCenter>;
}

function PageShell({ title, children }: { title: string; children: ReactNode }) {
  return <section className="grid h-dvh grid-rows-[auto_1fr] bg-background"><header className="flex h-16 items-center border-b border-border bg-card px-5"><h1 className="text-base font-semibold">{title}</h1></header><main className="min-h-0 overflow-y-auto p-6"><div className="mx-auto max-w-3xl">{children}</div></main></section>;
}

function PageCenter({ children }: { children: ReactNode }) {
  return <section className="grid h-dvh place-items-center bg-background p-8">{children}</section>;
}

export function WaNotFoundRoute() {
  return <Navigate to="/" replace />;
}

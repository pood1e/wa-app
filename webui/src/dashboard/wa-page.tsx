import { useMemo, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, Navigate, Outlet, useMatches, useNavigate, useOutletContext, useParams } from 'react-router';
import { toast } from 'sonner';
import { ArrowLeft, Loader2, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar';
import { Toaster } from '@/components/ui/sonner';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { deleteWaAccount, getWaAccounts, waAccountID, waKeys } from './wa-api';
import { WaAccountAdd } from './wa-account-add';
import { WaAccountInfoPage } from './wa-account-info-page';
import { WaAccountRail } from './wa-account-rail';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaInbox } from './wa-inbox';
import { useWaLongConnectionIndex } from './wa-long-connection-badge';
import { waChatsPath } from './wa-route-paths';

type WaRouteContext = { accounts: WAAccount[]; accountsLoading: boolean; accountsError: Error | null; connections: Map<string, LongConnectionState>; deleting: boolean; refreshAccounts: () => Promise<void>; refreshAccountAvatars: () => void; deleteAccount: (account: WAAccount) => void; done: (message: string) => void; error: (message: string) => void };

const emptyAccounts: WAAccount[] = [];
const accountSidebarStyle = { '--sidebar-width': '17rem', '--sidebar-width-icon': '4rem' } as CSSProperties;

export function WaLayout() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [accountAvatarVersion, setAccountAvatarVersion] = useState('');
  const [accountRailExpanded, setAccountRailExpanded] = useState(false);
  const accountsQuery = useInfiniteQuery({ queryKey: waKeys.accounts(), queryFn: ({ pageParam, signal }) => getWaAccounts(pageParam, signal), initialPageParam: '', getNextPageParam: (lastPage) => lastPage.next_cursor || undefined, refetchInterval: (query) => query.state.data ? 10000 : false });
  const connections = useWaLongConnectionIndex();
  const accounts = useMemo(() => accountsQuery.data?.pages.flatMap((page) => page.accounts || []) || emptyAccounts, [accountsQuery.data]);
  const selectedID = useSelectedAccountID(accounts);
  const deletion = useMutation({ mutationFn: deleteWaAccount, onSuccess: async () => { toast.success('本地账号已移除'); await refreshAccounts(); navigate('/', { replace: true }); }, onError: showErrorToast });
  async function refreshAccounts() {
    await queryClient.invalidateQueries({ queryKey: waKeys.accounts() });
  }
  const refreshAccountAvatars = () => setAccountAvatarVersion(String(Date.now()));
  const context: WaRouteContext = { accounts, accountsLoading: accountsQuery.isLoading, accountsError: accountsQuery.error, connections: connections.byAccount, deleting: deletion.isPending, refreshAccounts, refreshAccountAvatars, deleteAccount: deletion.mutate, done: toast.success, error: showErrorToast };
  return (
    <SidebarProvider open={accountRailExpanded} onOpenChange={setAccountRailExpanded} style={accountSidebarStyle} className="h-dvh min-h-0 overflow-hidden bg-background text-foreground">
      <WaAccountRail accounts={accounts} selectedID={selectedID} avatarVersion={accountAvatarVersion} connections={connections.byAccount} loading={accountsQuery.isLoading} connectionsLoading={connections.loading} hasNextPage={Boolean(accountsQuery.hasNextPage)} loadingMore={accountsQuery.isFetchingNextPage} onLoadMore={() => { void accountsQuery.fetchNextPage(); }} />
      <SidebarInset className="h-dvh min-w-0 overflow-hidden"><Outlet context={context} /></SidebarInset>
      <Toaster richColors closeButton />
    </SidebarProvider>
  );
}

export function WaHomeRoute() {
  const { accounts, accountsLoading, accountsError, refreshAccounts } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  if (accountsError && accounts.length === 0) return <AccountLoadError error={accountsError} onRetry={refreshAccounts} />;
  const firstID = waAccountID(accounts[0]);
  return firstID ? <Navigate to={waChatsPath(firstID)} replace /> : <NoAccount />;
}

export function WaCreateAccountRoute() {
  const { deleting, refreshAccounts, done, error } = useWaContext();
  return <PageShell title="添加账号"><WaAccountAdd disabled={deleting} onChanged={refreshAccounts} onDone={done} onError={error} /></PageShell>;
}

export function WaReloginAccountRoute() {
  const account = useRouteAccount();
  const navigate = useNavigate();
  const { accounts, accountsLoading, accountsError, deleting, refreshAccounts, done, error } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  if (accountsError && accounts.length === 0) return <AccountLoadError error={accountsError} onRetry={refreshAccounts} />;
  if (!account) return <AccountFallback accounts={accounts} />;
  async function accepted() {
    await refreshAccounts();
    navigate(waChatsPath(waAccountID(account)), { replace: true });
  }
  return <PageShell title="重新登录"><WaAccountAdd key={waAccountID(account)} account={account} disabled={deleting} onChanged={accepted} onDone={done} onError={error} /></PageShell>;
}

export function WaAccountInfoRoute() {
  const account = useRouteAccount();
  const { accounts, accountsLoading, accountsError, deleting, deleteAccount, done, error, refreshAccounts, refreshAccountAvatars } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载账号...</LoadingText></PageCenter>;
  if (accountsError && accounts.length === 0) return <AccountLoadError error={accountsError} onRetry={refreshAccounts} />;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaAccountInfoPage account={account} busy={deleting} onDelete={deleteAccount} onDone={done} onError={error} onAccountChanged={() => { void refreshAccounts(); }} onAvatarChanged={refreshAccountAvatars} />;
}

export function WaInboxRoute() {
  const { contactID = '' } = useParams();
  const account = useRouteAccount();
  const { accounts, accountsLoading, accountsError, refreshAccounts, connections } = useWaContext();
  if (accountsLoading) return <PageCenter><LoadingText>加载消息...</LoadingText></PageCenter>;
  if (accountsError && accounts.length === 0) return <AccountLoadError error={accountsError} onRetry={refreshAccounts} />;
  if (!account) return <AccountFallback accounts={accounts} />;
  return <WaInbox key={waAccountID(account)} account={account} connection={connections.get(waAccountID(account))} contactID={contactID} />;
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
  return (
    <PageCenter>
      <Empty className="max-w-sm border-0">
        <EmptyHeader>
          <EmptyMedia><WhatsAppIcon className="size-12" /></EmptyMedia>
          <EmptyTitle>还没有账号</EmptyTitle>
          <EmptyDescription>添加账号后即可查看联系人和消息。</EmptyDescription>
        </EmptyHeader>
        <EmptyContent><Button onClick={() => navigate('/accounts/new')}><Plus size={16} />添加账号</Button></EmptyContent>
      </Empty>
    </PageCenter>
  );
}

function PageShell({ title, children }: { title: string; children: ReactNode }) {
  return <section className="grid h-dvh grid-rows-[auto_1fr] bg-muted/45"><header className="flex h-16 items-center gap-4 border-b border-border bg-card px-4 sm:px-6"><Button asChild variant="ghost" size="icon" className="rounded-full" aria-label="返回聊天"><Link to="/"><ArrowLeft className="size-5" /></Link></Button><h1 className="text-base font-medium">{title}</h1></header><main className="min-h-0 overflow-y-auto px-4 py-6 sm:p-10"><div className="mx-auto max-w-xl">{children}</div></main></section>;
}

function PageCenter({ children }: { children: ReactNode }) {
  return <section className="grid h-dvh place-items-center bg-background p-8">{children}</section>;
}

function LoadingText({ children }: { children: ReactNode }) {
  return <span className="inline-flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{children}</span>;
}

function AccountLoadError({ error, onRetry }: { error: Error; onRetry: () => Promise<void> }) {
  return (
    <PageCenter>
      <Empty className="max-w-sm border-0" role="alert">
        <EmptyHeader><EmptyTitle>账号加载失败</EmptyTitle><EmptyDescription>{error.message}</EmptyDescription></EmptyHeader>
        <EmptyContent><Button onClick={() => void onRetry()}>重新加载</Button></EmptyContent>
      </Empty>
    </PageCenter>
  );
}

function showErrorToast(error: unknown) {
  toast.error(error instanceof Error ? error.message : String(error));
}

export function WaNotFoundRoute() {
  return <Navigate to="/" replace />;
}

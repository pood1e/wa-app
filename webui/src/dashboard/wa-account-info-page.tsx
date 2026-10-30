import { ArrowLeft } from 'lucide-react';
import { Link } from 'react-router';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID, waAccountTitle } from './wa-api';
import { WaAccountActions } from './wa-account-actions';
import { WaAccountDetail } from './wa-account-detail';
import { waChatsPath } from './wa-route-paths';

type Props = {
  account: WAAccount;
  busy: boolean;
  onDelete: (account: WAAccount) => void;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
  onAvatarChanged: () => void;
};

export function WaAccountInfoPage({ account, busy, onDelete, onDone, onError, onAccountChanged, onAvatarChanged }: Props) {
  const subtitle = waAccountTitle(account);
  return (
    <section className="grid h-dvh min-h-0 grid-rows-[auto_1fr] bg-muted/45">
      <header className="flex h-16 items-center justify-between gap-3 border-b border-border bg-card px-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <Link className="inline-flex size-9 shrink-0 items-center justify-center rounded-full transition hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring" to={waChatsPath(waAccountID(account))} title="返回消息" aria-label="返回消息"><ArrowLeft size={20} /></Link>
          <div className="min-w-0"><h1 className="truncate text-base font-medium">账号详情</h1><p className="truncate text-xs text-muted-foreground">{subtitle}</p></div>
        </div>
        <WaAccountActions key={waAccountID(account)} account={account} busy={busy} onDelete={onDelete} onDone={onDone} onError={onError} onAccountChanged={onAccountChanged} />
      </header>
      <main className="min-h-0 overflow-y-auto px-4 py-6 sm:p-8">
        <div className="mx-auto max-w-5xl">
          <WaAccountDetail account={account} onDone={onDone} onError={onError} onAccountChanged={onAccountChanged} onAvatarChanged={onAvatarChanged} />
        </div>
      </main>
    </section>
  );
}

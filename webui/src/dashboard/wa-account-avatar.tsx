import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { UserRound } from 'lucide-react';
import { waAccountProfilePictureURL, waAccountTitle } from './wa-api';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { WaAvatarImage } from './wa-avatar-image';

export function WaAccountAvatar({ account, version, size = 'md' }: { account: WAAccount; version: string; size?: 'xs' | 'sm' | 'md' | 'lg' }) {
  const src = waAccountProfilePictureURL(account, version || account.audit?.updated_at || 'latest');
  const sizeClass = size === 'xs' ? 'size-8' : size === 'sm' ? 'size-9' : size === 'lg' ? 'size-12' : 'size-10';
  const name = account.display_name?.trim();
  const title = waAccountTitle(account);
  return (
    <Avatar className={sizeClass}>
      {src ? <WaAvatarImage key={src} src={src} alt={title} /> : null}
      <AvatarFallback className="bg-accent text-sm font-medium text-accent-foreground">
        {name ? [...name].slice(0, 2).join('') : <UserRound className="size-5" strokeWidth={1.7} aria-hidden="true" />}
      </AvatarFallback>
    </Avatar>
  );
}

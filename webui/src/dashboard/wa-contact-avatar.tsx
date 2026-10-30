import type { WaContact } from './wa-chat-model';
import { Store, UserRound, UsersRound } from 'lucide-react';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import { WhatsAppIcon } from './wa-brand-icon';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { WaAvatarImage } from './wa-avatar-image';

type ContactAvatarSize = 'xs' | 'sm' | 'md';
type AvatarContact = Pick<WaContact, 'title' | 'kind' | 'profilePictureURL'>;

export function WaContactAvatar({ contact, size = 'md' }: { contact?: AvatarContact; size?: ContactAvatarSize }) {
  const pictureURL = contact?.profilePictureURL || '';
  const sizeClass = size === 'xs' ? 'size-8' : size === 'sm' ? 'size-10' : 'size-12';
  const title = contact?.title || '联系人';
  const Icon = contact?.kind === WAContactKind.WA_CONTACT_KIND_GROUP ? UsersRound : contact?.kind === WAContactKind.WA_CONTACT_KIND_BUSINESS ? Store : UserRound;
  return (
    <Avatar className={`${sizeClass} shrink-0`}>
      {pictureURL ? <WaAvatarImage src={pictureURL} alt={title} /> : null}
      <AvatarFallback className="bg-muted text-muted-foreground">
        {contact?.kind === WAContactKind.WA_CONTACT_KIND_SYSTEM ? <WhatsAppIcon className="size-7" title={title} /> : <Icon className={size === 'xs' ? 'size-5' : 'size-6'} strokeWidth={1.5} aria-hidden="true" />}
      </AvatarFallback>
    </Avatar>
  );
}

import { ChevronDown, Pencil, Reply, Trash2 } from 'lucide-react';
import { DropdownMenu } from 'radix-ui';
import type { WaChatMeta } from './wa-chat-model';
import { canQuoteMessage } from './wa-chat-model';
import { Button } from '@/components/ui/button';

export type WaMessageAction = 'edit' | 'revoke' | 'react';

export function WaMessageActions({ message, disabled, onReply, onEdit, onAction }: { message: WaChatMeta; disabled: boolean; onReply: () => void; onEdit: () => void; onAction: (type: WaMessageAction, id: string, text?: string) => Promise<unknown> }) {
  if (message.revoked) return null;
  const ownReaction = message.reactions.find((reaction) => reaction.sender_ref === 'self');
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Button variant="ghost" size="icon" className="absolute right-1 top-1 size-6 rounded-full bg-inherit text-muted-foreground opacity-100 sm:opacity-0 sm:group-hover:opacity-100 data-[state=open]:opacity-100 focus:opacity-100" aria-label="消息操作" disabled={disabled}><ChevronDown className="size-4" /></Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={4} className="z-50 min-w-44 rounded-lg border bg-popover p-1.5 text-sm text-popover-foreground shadow-lg">
          <div className="flex gap-1 border-b pb-1.5">
            {['👍', '❤️', '😂', '😮', '😢', '🙏'].map((text) => <DropdownMenu.Item key={text} className="cursor-pointer rounded p-1.5 text-lg outline-none focus:bg-accent" onSelect={() => void onAction('react', message.id, ownReaction?.text === text ? '' : text).catch(() => undefined)} aria-label={`回应 ${text}`}>{text}</DropdownMenu.Item>)}
          </div>
          {canQuoteMessage(message) ? <DropdownMenu.Item className="flex cursor-pointer items-center gap-2 rounded px-3 py-2 outline-none focus:bg-accent" onSelect={onReply}><Reply className="size-4" />回复</DropdownMenu.Item> : null}
          {message.outgoing && canQuoteMessage(message) ? <DropdownMenu.Item className="flex cursor-pointer items-center gap-2 rounded px-3 py-2 outline-none focus:bg-accent" onSelect={onEdit}><Pencil className="size-4" />编辑</DropdownMenu.Item> : null}
          {ownReaction ? <DropdownMenu.Item className="cursor-pointer rounded px-3 py-2 outline-none focus:bg-accent" onSelect={() => void onAction('react', message.id, '').catch(() => undefined)}>移除回应</DropdownMenu.Item> : null}
          {message.outgoing ? <DropdownMenu.Item className="flex cursor-pointer items-center gap-2 rounded px-3 py-2 text-destructive outline-none focus:bg-accent" onSelect={() => void onAction('revoke', message.id).catch(() => undefined)}><Trash2 className="size-4" />为所有人撤回</DropdownMenu.Item> : null}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export function WaMessageDetails({ message }: { message: WaChatMeta }) {
  if (message.revoked) return null;
  const quote = message.content?.quote;
  const reactions = new Map<string, number>();
  for (const reaction of message.reactions) reactions.set(reaction.text, (reactions.get(reaction.text) || 0) + 1);
  return (
    <>
      {quote ? <blockquote className="mb-1.5 rounded border-l-4 border-primary bg-foreground/5 px-2 py-1.5 text-xs"><span className="block font-medium text-primary">引用消息</span><span className="line-clamp-3 whitespace-pre-wrap text-muted-foreground">{quote.text?.value || quote.text?.redacted_value || '消息'}</span></blockquote> : null}
      {message.content?.view_once ? <p className="mb-1 text-xs text-muted-foreground">仅可查看一次</p> : null}
      {message.content?.ephemeral ? <p className="mb-1 text-xs text-muted-foreground">限时消息</p> : null}
      {reactions.size > 0 ? <div className="mt-1 flex flex-wrap gap-1" aria-label="消息回应">{[...reactions].map(([text, count]) => <span key={text} className="rounded-full bg-card px-2 py-0.5 text-xs">{text}{count > 1 ? ` ${count}` : ''}</span>)}</div> : null}
    </>
  );
}

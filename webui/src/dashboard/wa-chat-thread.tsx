import { useState } from 'react';
import type { FormEvent } from 'react';
import { AssistantRuntimeProvider, MessagePrimitive, ThreadPrimitive, useExternalStoreRuntime, useMessage, type AppendMessage } from '@assistant-ui/react';
import { Loader2, Send } from 'lucide-react';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaContactAvatar } from './wa-contact-avatar';
import { toAssistantMessage, type WaChatEvent, type WaChatMeta, type WaContact } from './wa-chat-model';
import { WaMessageContent } from './wa-message-content';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';

export function WaChatThread({ contact, events, loading, sending, error, onSendMessage }: { contact?: WaContact; events: WaChatEvent[]; loading: boolean; sending: boolean; error?: string; onSendMessage: (text: string) => Promise<unknown> }) {
  const runtime = useExternalStoreRuntime<WaChatEvent>({ messages: events, convertMessage: toAssistantMessage, isDisabled: true, isLoading: loading, onNew: noopNewMessage });
  const title = contact?.title || '选择联系人';
  return (
    <section className="grid min-h-0 grid-rows-[auto_1fr_auto] overflow-hidden bg-card">
      <ChatHeader contact={contact} loading={loading} />
      <div className="chat-doodle-surface h-full min-h-0">
        <AssistantRuntimeProvider runtime={runtime}>
          <ThreadPrimitive.Root className="h-full min-h-0">
            <ThreadPrimitive.Viewport autoScroll className="h-full min-h-0 space-y-3 overflow-y-auto p-5">
              <ThreadPrimitive.Empty><EmptyConversation title={title} /></ThreadPrimitive.Empty>
              <ThreadPrimitive.Messages>{() => <BubbleMessage />}</ThreadPrimitive.Messages>
            </ThreadPrimitive.Viewport>
          </ThreadPrimitive.Root>
        </AssistantRuntimeProvider>
      </div>
      <ChatComposer disabled={!contact || sending} error={error} onSendMessage={onSendMessage} />
    </section>
  );
}

function ChatHeader({ contact, loading }: { contact?: WaContact; loading: boolean }) {
  const subtitle = contact?.subtitle || '';
  return (
    <header className="flex h-16 items-center justify-between gap-3 border-b border-border px-5">
      <div className="flex min-w-0 items-center gap-3">
        <WaContactAvatar contact={contact} />
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{contact?.title || '暂无联系人'}</h2>
          {subtitle ? <p className="truncate text-xs text-muted-foreground">{subtitle}</p> : null}
        </div>
      </div>
      <div className="flex items-center gap-2">
        {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </div>
    </header>
  );
}

function BubbleMessage() {
  const meta = useMessage((message) => message.metadata.custom as WaChatMeta | undefined);
  const outgoing = Boolean(meta?.outgoing);
  const unread = Boolean(meta?.canMarkRead && !meta.read);
  return (
    <MessagePrimitive.Root className={`flex w-full ${outgoing ? 'justify-end' : 'justify-start'}`}>
      <div className={`flex max-w-[min(640px,82%)] flex-col ${outgoing ? 'items-end' : 'items-start'}`}>
        <div className={`w-fit max-w-full rounded-3xl border px-4 py-3 text-foreground shadow-sm ${outgoing ? 'rounded-tr-md border-transparent bg-chat-bubble-out' : unread ? 'rounded-tl-md border-transparent bg-chat-bubble-out' : 'rounded-tl-md border-border bg-card'}`}>
          {unread ? <div className="mb-1 flex items-center gap-2 text-[11px] text-muted-foreground"><Badge>未读</Badge></div> : null}
          <WaMessageContent text={meta?.displayText || ''} />
        </div>
        <MessageTime className="mt-1 px-2 text-[11px] text-muted-foreground" />
      </div>
    </MessagePrimitive.Root>
  );
}

function MessageTime({ className = '' }: { className?: string }) {
  const createdAt = useMessage((message) => message.createdAt);
  return createdAt ? <time className={className}>{createdAt.toLocaleString()}</time> : null;
}

function ChatComposer({ disabled, error, onSendMessage }: { disabled: boolean; error?: string; onSendMessage: (text: string) => Promise<unknown> }) {
  const [text, setText] = useState('');
  const trimmed = text.trim();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmed || disabled) return;
    try {
      await onSendMessage(trimmed);
      setText('');
    } catch {
      // React Query surfaces the error in the thread footer.
    }
  }
  return (
    <footer className="border-t border-border px-5 py-3">
      <form className="flex items-center gap-2" onSubmit={(event) => void submit(event)}>
        <Input value={text} onChange={(event) => setText(event.target.value)} disabled={disabled} placeholder={disabled ? '选择联系人后发送' : '输入消息'} aria-label="消息内容" autoComplete="off" />
        <Button size="icon" type="submit" disabled={disabled || !trimmed} title="发送" aria-label="发送"><Send size={16} /></Button>
      </form>
      {error ? <p className="mt-2 text-xs text-destructive">{error}</p> : null}
    </footer>
  );
}

function EmptyConversation({ title }: { title: string }) {
  return (
    <Empty className="mx-auto mt-16 max-w-sm flex-none border-0 bg-card/90 shadow-sm">
      <EmptyHeader>
        <EmptyMedia><WhatsAppIcon className="size-9" /></EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>选择联系人或等待新消息。</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

async function noopNewMessage(_message: AppendMessage) {}

import { useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { AssistantRuntimeProvider, MessagePrimitive, ThreadPrimitive, useExternalStoreRuntime, useMessage, type AppendMessage } from '@assistant-ui/react';
import { ArrowLeft, Loader2, MessageCircle, Send, RotateCw, X } from 'lucide-react';
import { Link } from 'react-router';
import { WhatsAppIcon } from './wa-brand-icon';
import { WaContactAvatar } from './wa-contact-avatar';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import { toAssistantMessage, type WaChatEvent, type WaChatMeta, type WaContact } from './wa-chat-model';
import { WaMessageContent } from './wa-message-content';
import { WaMessageActions, WaMessageDetails, type WaMessageAction } from './wa-message-actions';
import { WaMessageReceipt } from './wa-message-status';
import { waChatsPath, waReloginPath } from './wa-route-paths';
import { canonicalContactRef } from './wa-contact-aliases';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';

export function WaChatThread({ accountID, contact, events, loading, sending, readOnly, offline, rejected, error, hasOlder, loadingOlder, onLoadOlder, onRetry, onSendMessage, onMessageAction, acting, headerActions, unavailableReason }: { accountID: string; contact?: WaContact; events: WaChatEvent[]; loading: boolean; sending: boolean; readOnly: boolean; offline: boolean; rejected: boolean; error?: string; hasOlder: boolean; loadingOlder: boolean; onLoadOlder: () => Promise<unknown>; onRetry: () => Promise<unknown>; onSendMessage: (text: string, replyTo?: string) => Promise<unknown>; onMessageAction: (type: WaMessageAction, id: string, text?: string) => Promise<unknown>; acting: boolean; headerActions?: ReactNode; unavailableReason?: string }) {
  const [draft, setDraft] = useState('');
  const [target, setTarget] = useState<{ type: 'reply' | 'edit'; message: WaChatMeta; previousDraft?: string }>();
  const selectTarget = (type: 'reply' | 'edit', message: WaChatMeta) => {
    const previousDraft = target?.type === 'edit' ? target.previousDraft || '' : draft;
    setTarget({ type, message, previousDraft });
    setDraft(type === 'edit' ? message.copyText || message.displayText : previousDraft);
  };
  const submitMessage = async (text: string) => {
    if (target?.type === 'edit') await onMessageAction('edit', target.message.id, text);
    else await onSendMessage(text, target?.message.id);
    setDraft(target?.type === 'edit' ? target.previousDraft || '' : '');
    setTarget(undefined);
  };
  const runtime = useExternalStoreRuntime<WaChatEvent>({ messages: events, convertMessage: toAssistantMessage, isDisabled: true, isLoading: loading, onNew: noopNewMessage });
  const days = useMemo(() => messageDays(events), [events]);
  const viewport = useRef<HTMLDivElement>(null);
  const loadOlder = async () => {
    const element = viewport.current;
    const oldHeight = element?.scrollHeight || 0;
    const oldTop = element?.scrollTop || 0;
    await onLoadOlder();
    requestAnimationFrame(() => { if (element) element.scrollTop = oldTop + element.scrollHeight - oldHeight; });
  };
  if (!contact) return <ChatWelcome accountID={accountID} loading={loading} error={error} needsLogin={readOnly || rejected} />;
  return (
    <section aria-label="聊天内容" className="grid h-full min-h-0 grid-rows-[auto_1fr_auto] overflow-hidden bg-card">
      <ChatHeader accountID={accountID} contact={contact} loading={loading} actions={headerActions} />
      <div className="chat-doodle-surface h-full min-h-0">
        <AssistantRuntimeProvider runtime={runtime}>
          <ThreadPrimitive.Root className="h-full min-h-0">
            <ThreadPrimitive.Viewport ref={viewport} autoScroll className="h-full min-h-0 space-y-2 overflow-y-auto px-4 py-6 lg:px-[6%]">
              {offline && !readOnly ? <p role="status" className="mx-auto max-w-lg rounded-lg bg-card p-3 text-center text-xs text-muted-foreground">{rejected ? <>账号登录已失效，历史消息仍可查看。<Link className="ml-1 text-primary underline" to={waReloginPath(accountID)}>重新登录</Link></> : '连接尚未恢复，历史消息仍可查看。'}</p> : null}
              {error ? <div role="alert" className="mx-auto flex max-w-lg items-center justify-between gap-3 rounded-lg bg-card p-3 text-sm text-destructive"><span>{error}</span><Button variant="ghost" size="sm" onClick={() => void onRetry()}><RotateCw className="size-4" />重试</Button></div> : null}
              {hasOlder ? <div className="flex justify-center pb-3"><Button variant="secondary" size="sm" disabled={loadingOlder} onClick={() => void loadOlder()}>{loadingOlder ? <Loader2 className="size-4 animate-spin" /> : null}加载更早的消息</Button></div> : null}
              {!error ? <ThreadPrimitive.Empty><EmptyConversation loading={loading} /></ThreadPrimitive.Empty> : null}
              <ThreadPrimitive.Messages>{({ message }) => {
                const day = days.get(message.id);
                return <>{day ? <DaySeparator date={day} /> : null}<BubbleMessage disabled={readOnly || offline || sending || acting || Boolean(unavailableReason)} onTarget={selectTarget} onAction={onMessageAction} /></>;
              }}</ThreadPrimitive.Messages>
            </ThreadPrimitive.Viewport>
          </ThreadPrimitive.Root>
        </AssistantRuntimeProvider>
      </div>
      {readOnly || unavailableReason ? <p role="status" className="bg-muted px-4 py-5 text-center text-sm text-muted-foreground">{readOnly ? <>账号未登录，可查看历史消息。<Link className="ml-1 text-primary underline" to={waReloginPath(accountID)}>重新登录</Link></> : unavailableReason}</p> : <ChatComposer disabled={sending || acting || offline} sending={sending || acting} text={draft} onChange={setDraft} target={target} onCancel={() => { if (target?.type === 'edit') setDraft(target.previousDraft || ''); setTarget(undefined); }} onSendMessage={submitMessage} />}
    </section>
  );
}

function ChatHeader({ accountID, contact, loading, actions }: { accountID: string; contact: WaContact; loading: boolean; actions?: ReactNode }) {
  const subtitle = contact.subtitle === contact.title ? '' : contact.subtitle;
  return (
    <header className="flex h-16 items-center justify-between gap-3 bg-muted px-4">
      <div className="flex min-w-0 items-center gap-3">
        <Button asChild variant="ghost" size="icon" className="-ml-1 rounded-full md:hidden" aria-label="返回聊天列表"><Link to={waChatsPath(accountID)}><ArrowLeft className="size-5" /></Link></Button>
        <WaContactAvatar contact={contact} size="sm" />
        <div className="min-w-0">
          <h2 className="truncate text-[15px] font-medium">{contact.title}</h2>
          {subtitle ? <p className="truncate text-xs text-muted-foreground">{subtitle}</p> : null}
        </div>
      </div>
      <div className="flex items-center gap-2">
        {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        {actions}
      </div>
    </header>
  );
}

function BubbleMessage({ disabled, onTarget, onAction }: { disabled: boolean; onTarget: (type: 'reply' | 'edit', message: WaChatMeta) => void; onAction: (type: WaMessageAction, id: string, text?: string) => Promise<unknown> }) {
  const meta = useMessage((message) => message.metadata.custom as WaChatMeta | undefined);
  const outgoing = Boolean(meta?.outgoing);
  return (
    <MessagePrimitive.Root className={`flex w-full items-end gap-2 ${outgoing ? 'justify-end' : 'justify-start'}`}>
      {meta?.group && !outgoing ? <WaContactAvatar size="xs" contact={{ title: meta.sender, kind: WAContactKind.WA_CONTACT_KIND_USER, profilePictureURL: meta.senderPictureURL }} /> : null}
      <div className={`group relative flex min-w-0 max-w-[min(600px,88%)] flex-col rounded-lg px-3 pb-1.5 pt-2 text-foreground shadow-[0_1px_1px_#00000014] sm:max-w-[min(600px,80%)] ${outgoing ? 'rounded-tr-none bg-chat-bubble-out' : 'rounded-tl-none bg-chat-bubble-in'}`}>
        {meta?.group && !outgoing ? <div data-message-sender className="mb-1 flex flex-wrap items-baseline gap-x-2 pr-5 text-xs leading-4"><span className={`font-medium ${senderColor(meta.senderRef)}`}>{meta.sender}</span>{meta.senderDetail ? <span className="text-muted-foreground">{meta.senderDetail}</span> : null}</div> : null}
        {meta ? <WaMessageActions message={meta} disabled={disabled} onReply={() => onTarget('reply', meta)} onEdit={() => onTarget('edit', meta)} onAction={onAction} /> : null}
        {meta ? <WaMessageDetails message={meta} /> : null}
        {meta?.revoked ? <p className="pr-4 text-sm italic text-muted-foreground">这条消息已撤回</p> : <WaMessageContent text={meta?.displayText || ''} />}
        <div className="mt-0.5 flex items-center gap-1 self-end pl-4 text-[10px] leading-4 text-foreground/55">{meta?.edited && !meta.revoked ? <span>已编辑</span> : null}<MessageTime />{outgoing && meta?.status ? <WaMessageReceipt status={meta.status} /> : null}</div>
      </div>
    </MessagePrimitive.Root>
  );
}

function senderColor(ref: string) {
  const colors = ['text-emerald-700 dark:text-emerald-400', 'text-sky-700 dark:text-sky-400', 'text-violet-700 dark:text-violet-400', 'text-orange-700 dark:text-orange-400', 'text-pink-700 dark:text-pink-400', 'text-teal-700 dark:text-teal-400'];
  let hash = 0;
  for (const character of canonicalContactRef(ref)) hash = (Math.imul(hash, 31) + character.charCodeAt(0)) >>> 0;
  return colors[hash % colors.length];
}

function MessageTime({ className = '' }: { className?: string }) {
  const createdAt = useMessage((message) => message.createdAt);
  return createdAt ? <time className={className} dateTime={createdAt.toISOString()} title={createdAt.toLocaleString('zh-CN')}>{createdAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</time> : null;
}

function messageDays(events: WaChatEvent[]) {
  const days = new Map<string, Date>();
  let previousDay = '';
  for (const event of events) {
    const day = event.at?.toDateString() || '';
    if (event.at && day !== previousDay) days.set(event.id, event.at);
    previousDay = day;
  }
  return days;
}

function DaySeparator({ date }: { date: Date }) {
  const today = new Date();
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  const label = date.toDateString() === today.toDateString() ? '今天' : date.toDateString() === yesterday.toDateString() ? '昨天' : date.toLocaleDateString('zh-CN');
  return <div className="flex justify-center pb-3 pt-1"><time className="rounded-lg bg-card/90 px-3 py-1.5 text-[11px] text-muted-foreground shadow-sm" dateTime={date.toISOString()}>{label}</time></div>;
}

function ChatComposer({ disabled, sending, text, onChange, target, onCancel, onSendMessage }: { disabled: boolean; sending: boolean; text: string; onChange: (text: string) => void; target?: { type: 'reply' | 'edit'; message: WaChatMeta }; onCancel: () => void; onSendMessage: (text: string) => Promise<unknown> }) {
  const trimmed = text.trim();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmed || disabled) return;
    try { await onSendMessage(trimmed); } catch { /* The thread displays the mutation error and retains the draft. */ }
  }
  return (
    <footer className="bg-muted px-3 py-3 sm:px-4">
      {target ? <div className="mb-2 flex items-center justify-between gap-2 rounded border-l-4 border-primary bg-card px-3 py-2 text-sm"><div className="min-w-0"><p className="font-medium text-primary">{target.type === 'edit' ? '编辑消息' : `回复 ${target.message.outgoing ? '自己' : [target.message.sender, target.message.senderDetail].filter(Boolean).join(' · ')}`}</p><p className="truncate text-xs text-muted-foreground">{target.message.displayText}</p></div><Button variant="ghost" size="icon" onClick={onCancel} aria-label="取消引用或编辑" disabled={sending}><X className="size-4" /></Button></div> : null}
      <form className="flex items-center gap-2" onSubmit={(event) => void submit(event)}>
        <Input className="h-12 min-w-0 rounded-full border-transparent bg-card px-5 text-[15px] shadow-none focus-visible:ring-2 dark:bg-card" value={text} onChange={(event) => onChange(event.target.value)} disabled={sending} placeholder={sending ? '正在发送…' : disabled ? '等待连接，可先编辑消息' : '输入消息'} aria-label="消息内容" autoComplete="off" />
        <Button className="size-11 rounded-full disabled:bg-card disabled:text-muted-foreground disabled:opacity-100" size="icon" type="submit" disabled={disabled || !trimmed} title={target?.type === 'edit' ? '保存编辑' : '发送'} aria-label={target?.type === 'edit' ? '保存编辑' : '发送'}>{sending ? <Loader2 className="size-5 animate-spin" /> : <Send className="size-5" />}</Button>
      </form>
    </footer>
  );
}

function EmptyConversation({ loading }: { loading: boolean }) {
  return (
    <Empty className="mx-auto mt-8 max-w-sm flex-none border-0 px-5 py-6">
      <EmptyHeader>
        <EmptyMedia>{loading ? <Loader2 className="size-6 animate-spin text-muted-foreground" /> : <MessageCircle className="size-6 text-muted-foreground" />}</EmptyMedia>
        <EmptyTitle className="text-sm">{loading ? '正在加载消息' : '还没有消息'}</EmptyTitle>
        <EmptyDescription>{loading ? '稍等片刻。' : '发送第一条消息，开始对话。'}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

function ChatWelcome({ accountID, loading, error, needsLogin }: { accountID: string; loading: boolean; error?: string; needsLogin: boolean }) {
  return (
    <section className="relative flex h-full flex-col items-center justify-center border-b-[5px] border-primary/70 bg-chat-idle px-8 text-center">
      <Button asChild variant="ghost" size="icon" className="absolute left-4 top-4 rounded-full md:hidden" aria-label="返回聊天列表"><Link to={waChatsPath(accountID)}><ArrowLeft className="size-5" /></Link></Button>
      <div className="mb-7 grid size-28 place-items-center rounded-full bg-accent/70">{loading ? <Loader2 className="size-10 animate-spin text-primary" /> : <WhatsAppIcon className="size-16 opacity-80" />}</div>
      <h2 className="text-[30px] font-light tracking-tight">{loading ? '正在加载聊天' : error ? '无法加载聊天' : 'WhatsApp'}</h2>
      <p role={error ? 'alert' : undefined} className="mt-4 max-w-sm text-sm leading-6 text-muted-foreground">{error || (loading ? '稍等片刻。' : '选择一个聊天，查看消息或开始对话。')}</p>
      {!loading && !error ? <p className="mt-2 text-xs text-muted-foreground/80">点击左侧头像可切换账号</p> : null}
      {needsLogin ? <Button className="mt-5" asChild><Link to={waReloginPath(accountID)}>重新登录</Link></Button> : null}
    </section>
  );
}

async function noopNewMessage(_message: AppendMessage) {}

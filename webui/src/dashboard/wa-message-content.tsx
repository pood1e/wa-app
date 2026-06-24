import { ExternalLink } from 'lucide-react';
import { normalizeWaMessageText } from './wa-message-rich-text';

const RICH_PREFIXES = [
  '[商品]',
  '[模板]',
  '[模板回复]',
  '[图片]',
  '[视频]',
  '[圆形视频]',
  '[文件]',
  '[语音]',
  '[联系人]',
  '[位置]',
  '[实时位置]',
  '[按钮]',
  '[按钮回复]',
  '[互动]',
  '[互动回复]',
  '[列表]',
  '[列表回复]',
  '[投票]',
  '[订单]',
  '[回应]',
];

export function WaMessageContent({ text }: { text: string }) {
  const displayText = normalizeWaMessageText(text);
  return <MessageLines text={stripRichPrefix(displayText)} />;
}

function MessageLines({ text }: { text: string }) {
  const lines = text.split(/\r?\n/).filter((line) => line.trim() !== '');
  const firstURL = firstLink(lines);
  if (lines.length === 0) return <p className="text-sm text-muted-foreground">空消息</p>;
  return (
    <div className="space-y-1 text-sm leading-6 text-foreground">
      {lines.map((line, index) => (
        <p className="break-words" key={`${index}-${line}`}>
          {linkify(line)}
        </p>
      ))}
      {firstURL ? <LinkPreview url={firstURL} /> : null}
    </div>
  );
}

function linkify(line: string) {
  const tokens = line.split(/(https?:\/\/\S+)/g);
  return tokens.map((token, index) => {
    if (!/^https?:\/\//.test(token)) return markdownInline(token, `text-${index}`);
    const url = trimLink(token);
    return (
      <a className="text-emerald-700 underline underline-offset-2" href={url} key={`${index}-${token}`} rel="noreferrer" target="_blank">
        {url}
      </a>
    );
  });
}

function markdownInline(text: string, keyPrefix: string) {
  const parts = text.split(/(\*[^*\n]{1,120}\*)/g);
  return parts.map((part, index) => {
    const match = part.match(/^\*([^*\n].*?)\*$/);
    if (!match) return part;
    return <strong className="font-semibold text-foreground" key={`${keyPrefix}-${index}`}>{match[1]}</strong>;
  });
}

function LinkPreview({ url }: { url: string }) {
  const host = linkHost(url);
  return (
    <a className="mt-2 flex max-w-sm items-center gap-3 rounded-2xl border border-border bg-card p-3 text-foreground transition hover:bg-accent" href={url} rel="noreferrer" target="_blank">
      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
        <ExternalLink size={16} />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{host || '打开链接'}</span>
        <span className="block truncate text-xs text-muted-foreground">{url}</span>
      </span>
    </a>
  );
}

function stripRichPrefix(text: string) {
  const value = text.trim();
  for (const prefix of RICH_PREFIXES) {
    if (value === prefix) return '';
    if (value.startsWith(`${prefix} `)) return value.slice(prefix.length).trim();
  }
  return text;
}

function firstLink(lines: string[]) {
  for (const line of lines) {
    const match = line.match(/https?:\/\/\S+/);
    if (match?.[0]) return trimLink(match[0]);
  }
  return '';
}

function linkHost(url: string) {
  try {
    return new URL(url).host.replace(/^www\./, '');
  } catch {
    return '';
  }
}

function trimLink(url: string) {
  return url.replace(/[),.;!?]+$/, '');
}

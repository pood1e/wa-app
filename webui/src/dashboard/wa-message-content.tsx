import { ExternalLink } from 'lucide-react';
export function WaMessageContent({ text }: { text: string }) {
  return <MessageLines text={text} />;
}

function MessageLines({ text }: { text: string }) {
  const lines = text.split(/\r?\n/).filter((line) => line.trim() !== '');
  const firstURL = firstLink(lines);
  if (lines.length === 0) return <p className="text-sm text-muted-foreground">空消息</p>;
  return (
    <div className="space-y-1 text-[14px] leading-[22px] text-foreground [overflow-wrap:anywhere]">
      {lines.map((line, index) => (
        <p className="whitespace-pre-wrap break-words" key={`${index}-${line}`}>
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
      <a className="text-primary underline underline-offset-2 dark:text-emerald-300" href={url} key={`${index}-${token}`} rel="noreferrer" target="_blank">
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
    <a className="mt-2 flex max-w-sm items-center gap-3 rounded-md bg-foreground/5 p-3 text-foreground transition hover:bg-foreground/10" href={url} rel="noreferrer" target="_blank">
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

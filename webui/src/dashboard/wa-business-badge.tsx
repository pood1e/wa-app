import { WAAccountType } from '../proto/byte/v/forge/waapp/model/v1/account_type';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

type Props = { accountType: WAAccountType; compact?: boolean; className?: string };

export function WaBusinessBadge({ accountType, compact = false, className }: Props) {
  if (accountType !== WAAccountType.WA_ACCOUNT_TYPE_BUSINESS) return null;
  return (
    <Badge
      variant="outline"
      className={cn(
        compact
          ? 'size-4 rounded-full border-0 bg-emerald-700 p-0 text-[10px] leading-none text-white dark:bg-emerald-400 dark:text-emerald-950'
          : 'border-emerald-600/20 bg-emerald-500/10 text-emerald-800 dark:border-emerald-400/25 dark:text-emerald-300',
        className,
      )}
      title="WhatsApp Business · 商业账号"
      aria-label="WhatsApp Business 商业账号"
    >
      <span className="font-bold" aria-hidden="true">B</span>
      {!compact ? '商业账号' : null}
    </Badge>
  );
}

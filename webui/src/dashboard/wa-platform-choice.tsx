import { ClientPlatform } from '../proto/byte/v/forge/waapp/protocol/v1/account';
import { Button } from '@/components/ui/button';

type Props = { platform: ClientPlatform; disabled?: boolean; onChange: (platform: ClientPlatform) => void };

export function WaPlatformChoice({ platform, disabled, onChange }: Props) {
  return (
    <div role="group" aria-label="协议平台" className="flex items-center gap-2">
      <span className="mr-auto text-xs text-muted-foreground">协议平台</span>
      {[
        { value: ClientPlatform.CLIENT_PLATFORM_ANDROID, label: 'Android' },
        { value: ClientPlatform.CLIENT_PLATFORM_IOS, label: 'iOS' },
      ].map((option) => <Button className="h-8 rounded-full px-4" key={option.value} size="sm" aria-pressed={platform === option.value} variant={platform === option.value ? 'secondary' : 'ghost'} disabled={disabled} onClick={() => onChange(option.value)} type="button">{option.label}</Button>)}
    </div>
  );
}

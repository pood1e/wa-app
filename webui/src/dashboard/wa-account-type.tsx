import { useState, type FormEvent } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { Loader2 } from 'lucide-react';
import { WAAccountType } from '../proto/byte/v/forge/waapp/model/v1/account_type';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { ClientPlatform } from '../proto/byte/v/forge/waapp/protocol/v1/account';
import { beginAccountAdd } from './wa-account-add-api';
import { waReloginPath } from './wa-route-paths';
import { WaPlatformChoice } from './wa-platform-choice';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

type Props = {
  account: WAAccount;
  online: boolean;
  onError: (message: string) => void;
};

export function WaAccountTypeSettings({ account, online, onError }: Props) {
  const [name, setName] = useState(account.display_name || '');
  const [platform, setPlatform] = useState(ClientPlatform.CLIENT_PLATFORM_ANDROID);
  const navigate = useNavigate();
  const mutation = useMutation({
    mutationFn: async () => {
      const response = await beginAccountAdd({ phone: account.phone, platform, source: online ? { transfer: {} } : { registration: {} }, proxy: undefined, business: { name: name.trim() } });
      if (!response.flow) throw new Error(response.error?.message || '创建转换流程失败');
      return response;
    },
    onSuccess: (response) => { navigate(`${waReloginPath(account.wa_account_id)}?flow=${encodeURIComponent(response.flow!.flow_id)}`); },
    onError: (error) => onError(error instanceof Error ? error.message : String(error)),
  });
  if (account.account_type === WAAccountType.WA_ACCOUNT_TYPE_BUSINESS) {
    return <p className="text-sm">商业账号 · {account.display_name}</p>;
  }
  const submit = (event: FormEvent) => { event.preventDefault(); mutation.mutate(); };
  return (
    <form className="grid gap-3" onSubmit={submit}>
      <p className="text-xs leading-5 text-muted-foreground">验证当前号码后转为 WhatsApp Business，保留账号资料和聊天记录。</p>
      <WaPlatformChoice platform={platform} disabled={mutation.isPending} onChange={setPlatform} />
      <div className="flex flex-wrap items-center gap-3">
        <Input className="min-w-40 flex-1" aria-label="商家名称" placeholder="商家名称" value={name} maxLength={256} disabled={mutation.isPending} onChange={(event) => setName(event.target.value)} />
        <Button type="submit" disabled={!name.trim() || mutation.isPending}>
          {mutation.isPending ? <><Loader2 className="size-4 animate-spin" />正在转换</> : '转为商业账号'}
        </Button>
      </div>
    </form>
  );
}

import { type FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import type { AccountProxy } from '../proto/byte/v/forge/waapp/v1/account_proxy';
import { getWaAccountProxy, setWaAccountProxy, waKeys } from './wa-api';
import { WaAccountProxyFields } from './wa-account-proxy-fields';
import { validAccountProxy } from './wa-account-proxy-model';

type Props = { accountID: string; onDone: (message: string) => void; onError: (message: string) => void };

export function WaAccountProxySettings(props: Props) {
  const query = useQuery({ queryKey: waKeys.proxy(props.accountID), queryFn: ({ signal }) => getWaAccountProxy(props.accountID, signal), refetchOnWindowFocus: false });
  if (query.isPending) return <p role="status" className="text-sm text-muted-foreground">正在读取代理配置…</p>;
  if (query.isError) return <div className="grid justify-items-start gap-2"><p role="alert" className="text-sm text-destructive">{query.error.message}</p><Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()}>重新加载</Button></div>;
  return <ProxyForm key={`${props.accountID}/${JSON.stringify(query.data.proxy)}`} {...props} stored={query.data.proxy} />;
}

function ProxyForm({ accountID, stored, onDone, onError }: Props & { stored?: AccountProxy }) {
  const queryClient = useQueryClient();
  const [value, setValue] = useState<AccountProxy | undefined>(() => stored ? { ...stored, password: undefined } : undefined);
  const mutation = useMutation({
    mutationFn: () => setWaAccountProxy(accountID, value),
    onSuccess: (response) => {
      setValue(response.proxy ? { ...response.proxy, password: undefined } : undefined);
      queryClient.setQueryData(waKeys.proxy(accountID), response);
      onDone(response.proxy ? '账号代理已保存，连接将使用新代理' : '已恢复全局默认代理');
    },
    onError: (error) => onError(error.message),
    onSettled: () => { void queryClient.invalidateQueries({ queryKey: waKeys.proxy(accountID) }); void queryClient.invalidateQueries({ queryKey: ['wa', 'connections'] }); },
  });
  function submit(event: FormEvent) { event.preventDefault(); if (validAccountProxy(value)) mutation.mutate(); }
  return (
    <form className="grid gap-4" onSubmit={submit}>
      <p className="text-xs leading-5 text-muted-foreground">用于该账号的注册、转移和后续连接。默认继承全局代理；未配置全局代理时直连。</p>
      <WaAccountProxyFields value={value} stored={stored} disabled={mutation.isPending} onChange={setValue} />
      <Button className="justify-self-start rounded-full" type="submit" disabled={mutation.isPending || !validAccountProxy(value)}>{mutation.isPending ? '正在保存…' : '保存代理配置'}</Button>
    </form>
  );
}

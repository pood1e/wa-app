import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ClipboardCheck, Clock3, Loader2, RefreshCw, ShieldCheck, ShieldX } from 'lucide-react';
import { AccountReviewStatus } from '../proto/byte/v/forge/waapp/protocol/v1/account_review';
import { getWaAccountReview, refreshWaAccountReview, submitWaAccountReview, waKeys } from './wa-api';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';

type Props = { accountID: string };

export function WaAccountReview({ accountID }: Props) {
  const [description, setDescription] = useState('');
  const client = useQueryClient();
  const key = waKeys.review(accountID);
  const query = useQuery({ queryKey: key, queryFn: ({ signal }) => getWaAccountReview(accountID, signal), retry: false });
  const refresh = useMutation({
    mutationFn: () => refreshWaAccountReview(accountID),
    onSuccess: (response) => client.setQueryData(key, response),
  });
  const submit = useMutation({
    mutationFn: () => submitWaAccountReview(accountID, description.trim()),
    retry: false,
    onSuccess: (response) => { client.setQueryData(key, response); setDescription(''); },
    onSettled: () => client.invalidateQueries({ queryKey: key }),
  });
  const status = query.data?.review?.status ?? AccountReviewStatus.ACCOUNT_REVIEW_STATUS_UNSPECIFIED;
  const busy = refresh.isPending || submit.isPending;
  const error = refresh.error || submit.error || query.error;
  const checkedAt = query.data?.review?.checked_at;
  const StatusIcon = status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_APPROVED ? ShieldCheck : status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_REJECTED ? ShieldX : status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_IN_REVIEW ? Clock3 : ClipboardCheck;
  if (status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_UNAVAILABLE) return null;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!busy && description.trim()) submit.mutate();
  }

  return (
    <Card className="gap-5 rounded-xl py-5 ring-border" size="sm">
      <CardHeader className="flex items-center justify-between gap-3 px-5">
        <CardTitle className="inline-flex items-center gap-2 text-sm text-primary"><ClipboardCheck size={16} />账号复核</CardTitle>
        <Badge className="rounded-full px-2.5 py-1" aria-live="polite" variant={status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_REJECTED ? 'destructive' : 'secondary'}>{reviewLabel(status)}</Badge>
      </CardHeader>
      <CardContent className="grid gap-4 px-5">
        <div className="flex items-center gap-3 rounded-lg bg-muted/60 p-4">
          <span className={`grid size-10 shrink-0 place-items-center rounded-full ${status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_REJECTED ? 'bg-destructive/10 text-destructive' : 'bg-accent text-accent-foreground'}`}>{query.isLoading ? <Loader2 className="size-5 animate-spin" /> : <StatusIcon className="size-5" strokeWidth={1.6} />}</span>
          <p className="text-sm leading-6 text-muted-foreground">{reviewDetail(status)}</p>
        </div>
        {error ? <p role="alert" className="text-sm text-destructive">{error.message}</p> : null}
        {status === AccountReviewStatus.ACCOUNT_REVIEW_STATUS_NOT_REQUESTED ? (
          <form className="grid gap-3" onSubmit={onSubmit}>
            <Label htmlFor={`review-${accountID}`}>复核说明</Label>
            <textarea
              id={`review-${accountID}`}
              className="min-h-28 w-full resize-y rounded-lg border border-input bg-transparent p-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              maxLength={1000}
              disabled={busy}
              placeholder="说明账号的使用情况和申请复核的原因"
            />
            <Button className="h-10 rounded-full" type="submit" disabled={busy || !description.trim()}>
              {submit.isPending ? <Loader2 size={14} className="animate-spin" /> : <ClipboardCheck size={14} />}
              提交复核
            </Button>
          </form>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
          <span>{checkedAt ? `最近查询 ${new Date(checkedAt).toLocaleString('zh-CN')}` : '尚未查询远程结果'}</span>
          <Button className="h-8 rounded-full text-primary" size="sm" variant="outline" disabled={busy || query.isLoading} onClick={() => { submit.reset(); refresh.mutate(); }}>
            <RefreshCw size={14} className={refresh.isPending ? 'animate-spin' : ''} />
            {refresh.isPending ? '查询中' : '刷新状态'}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function reviewLabel(status: AccountReviewStatus) {
  switch (status) {
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_NOT_REQUESTED: return '尚未提交';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_IN_REVIEW: return '审核中';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_REJECTED: return '维持限制';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_APPROVED: return '已解除限制';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN: return '结果待确认';
    default: return '待查询';
  }
}

function reviewDetail(status: AccountReviewStatus) {
  switch (status) {
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_NOT_REQUESTED: return '填写使用情况，向 WhatsApp 申请复核当前账号限制。';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_IN_REVIEW: return '复核已受理，等待审核结果。';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_REJECTED: return '本次复核结果为维持账号限制。';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_APPROVED: return '账号限制已解除，重新登录后可验证是否恢复使用。';
    case AccountReviewStatus.ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN: return '暂时无法确认提交结果，请先刷新状态。';
    default: return '查询当前账号是否可申请复核，以及已有申请的处理结果。';
  }
}

import { Check, CheckCheck, Clock3, CircleAlert } from 'lucide-react';
import type { AccountMessage } from '../proto/byte/v/forge/waapp/v1/messaging';
import { MessageAckStatus, MessageReceiptType } from '../proto/byte/v/forge/waapp/v1/messaging';

export type WaMessageStatus = { state: 'pending' | 'sent' | 'delivered' | 'read' | 'failed'; label: string; group: boolean; readCount: number };

export function messageStatus(message: AccountMessage): WaMessageStatus {
  const group = message.contact_ref?.endsWith('@g.us') || false;
  const delivered = new Set<string>();
  const read = new Set<string>();
  for (const receipt of message.receipts || []) {
    if (!receipt.recipient_ref) continue;
    if (receipt.type === MessageReceiptType.MESSAGE_RECEIPT_TYPE_DELIVERED || receipt.type === MessageReceiptType.MESSAGE_RECEIPT_TYPE_READ || receipt.type === MessageReceiptType.MESSAGE_RECEIPT_TYPE_PLAYED) delivered.add(receipt.recipient_ref);
    if (receipt.type === MessageReceiptType.MESSAGE_RECEIPT_TYPE_READ || receipt.type === MessageReceiptType.MESSAGE_RECEIPT_TYPE_PLAYED) read.add(receipt.recipient_ref);
  }
  const state = read.size ? 'read' : delivered.size ? 'delivered' : message.ack_status === MessageAckStatus.MESSAGE_ACK_STATUS_FAILED ? 'failed' : message.ack_status === MessageAckStatus.MESSAGE_ACK_STATUS_ACKED ? 'sent' : 'pending';
  const label = group && read.size ? `${read.size} 人已读` : group && delivered.size ? `已送达 ${delivered.size} 人` : state === 'read' ? '已读' : state === 'delivered' ? '已送达' : state === 'sent' ? '已发送' : state === 'failed' ? '发送失败' : '等待确认';
  return { state, label, group, readCount: read.size };
}

export function WaMessageReceipt({ status }: { status: WaMessageStatus }) {
  const Icon = status.state === 'pending' ? Clock3 : status.state === 'failed' ? CircleAlert : status.state === 'sent' ? Check : CheckCheck;
  const color = status.state === 'read' && !status.group ? 'text-sky-500' : status.state === 'failed' ? 'text-destructive' : 'text-foreground/55';
  return <span role="img" aria-label={status.label} title={status.label} className={`inline-flex items-center gap-1 ${color}`}><Icon className="size-[15px]" strokeWidth={1.7} aria-hidden="true" />{status.group && status.readCount > 0 ? <span className="text-[10px]">{status.label}</span> : null}</span>;
}

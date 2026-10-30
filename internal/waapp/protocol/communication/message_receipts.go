package communication

import (
	"slices"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"go.mau.fi/whatsmeow/types"
)

func (s *ChatdSession) messageReceipts(node wire.ChatdNode) []*waappv1.MessageReceipt {
	if node.Tag != "receipt" {
		return nil
	}
	source, ok := s.messageSource(node)
	if !ok || source.fromMe {
		return nil
	}
	ids := receiptMessageIDs(node)
	var receipts []*waappv1.MessageReceipt
	aggregated := false
	for _, participants := range wire.ChatdChildren(node) {
		if participants.Tag != "participants" {
			continue
		}
		aggregated = true
		messageIDs := ids
		if id := participants.Attrs["message_id"]; id != "" {
			messageIDs = []string{id}
		}
		for _, user := range wire.ChatdChildren(participants) {
			if user.Tag != "user" {
				continue
			}
			kind, timestamp := node.Attrs["type"], node.Attrs["t"]
			if value, exists := user.Attrs["type"]; exists {
				kind = value
			}
			if value, exists := user.Attrs["t"]; exists {
				timestamp = value
			}
			receipts = s.appendReceipts(receipts, source.chat, userJID(user.Attrs["jid"]), messageIDs, kind, timestamp)
		}
	}
	if !aggregated {
		receipts = s.appendReceipts(receipts, source.chat, source.sender, ids, node.Attrs["type"], node.Attrs["t"])
	}
	return receipts
}

func (s *ChatdSession) appendReceipts(dst []*waappv1.MessageReceipt, chat, sender types.JID, ids []string, kind, timestamp string) []*waappv1.MessageReceipt {
	if sender.IsEmpty() || s.ownJID(sender) {
		return dst
	}
	typeValue := receiptType(kind)
	stamp := ChatdProviderTimestamp(timestamp)
	if typeValue == waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_UNSPECIFIED || stamp == nil || stamp.CheckValid() != nil {
		return dst
	}
	for _, id := range ids {
		dst = append(dst, &waappv1.MessageReceipt{ProviderMessageId: id, ContactRef: chat.String(), RecipientRef: sender.ToNonAD().String(), Type: typeValue, Timestamp: stamp})
	}
	return dst
}

func receiptMessageIDs(node wire.ChatdNode) []string {
	var ids []string
	if id := node.Attrs["id"]; id != "" {
		ids = append(ids, id)
	}
	if list, ok := ChatdChild(node, "list"); ok {
		for _, item := range wire.ChatdChildren(list) {
			if id := item.Attrs["id"]; item.Tag == "item" && id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func receiptType(value string) waappv1.MessageReceiptType {
	switch value {
	case "", "delivery":
		return waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED
	case "read":
		return waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ
	case "played":
		return waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_PLAYED
	default:
		return waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_UNSPECIFIED
	}
}

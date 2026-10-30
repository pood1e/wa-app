package account

import (
	"context"
	"slices"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *serverCore) attachMessageReceipts(ctx context.Context, accountID string, messages []*waappv1.AccountMessage) error {
	var ids []string
	byID := make(map[string][]*waappv1.AccountMessage)
	for _, message := range messages {
		if message.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND && message.GetProviderMessageId() != "" {
			id := message.GetProviderMessageId()
			ids = append(ids, id)
			byID[id] = append(byID[id], message)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	receipts, err := s.store.ListMessageReceipts(ctx, accountID, ids)
	if err != nil {
		return err
	}
	aliases := make(map[string][]string)
	for _, receipt := range receipts {
		for _, message := range byID[receipt.GetProviderMessageId()] {
			refs, ok := aliases[message.GetContactRef()]
			if !ok {
				refs = s.resolveContactActionRefs(ctx, accountID, message.GetContactRef())
				aliases[message.GetContactRef()] = refs
			}
			if slices.Contains(refs, receipt.GetContactRef()) {
				message.Receipts = append(message.Receipts, receipt)
			}
		}
	}
	return nil
}

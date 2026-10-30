package account

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) attachMessageChanges(ctx context.Context, accountID string, refs []string, messages []*waappv1.AccountMessage, sensitive bool) error {
	targets := make([]string, 0, len(messages))
	for _, message := range messages {
		targets = append(targets, message.GetProviderMessageId())
	}
	changes, err := s.store.ListMessageChanges(ctx, accountID, refs, targets, sensitive)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	aliases := make(map[string]string)
	canonical := func(ref string) string {
		if ref == "" {
			return ""
		}
		if jid, err := types.ParseJID(ref); err == nil && !jid.IsEmpty() {
			ref = jid.ToNonAD().String()
		}
		if value, exists := aliases[ref]; exists {
			return value
		}
		value := ref
		for _, alias := range s.resolveContactActionRefs(ctx, accountID, ref) {
			if strings.HasSuffix(alias, "@lid") {
				value = alias
				break
			}
		}
		aliases[ref] = value
		return value
	}
	for _, event := range changes {
		event.ContactRef, event.SenderRef = canonical(event.GetContactRef()), canonical(event.GetSenderRef())
		if change := event.GetContent().GetChange(); change != nil {
			if change.TargetSenderRef != "" {
				change.TargetSenderRef = canonical(change.TargetSenderRef)
			}
			if change.TargetContactRef != "" {
				change.TargetContactRef = canonical(change.TargetContactRef)
			}
		}
	}
	for _, target := range messages {
		chat, sender := target.GetContactRef(), target.GetSenderRef()
		target.ContactRef, target.SenderRef = canonical(chat), canonical(sender)
		wamodel.ApplyMessageChanges([]*waappv1.AccountMessage{target}, changes)
		target.ContactRef, target.SenderRef = chat, sender
	}
	return nil
}

package account

import (
	"context"
	"slices"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) enrichContactPreviews(ctx context.Context, accountID string, contacts []*waappv1.WAContact) error {
	var refs []string
	for _, contact := range contacts {
		refs = append(refs, wamodel.ContactMessageRefs(contact)...)
	}
	refs = shared.UniqueNonEmptyStrings(refs...)
	messages, err := s.store.ListLatestAccountMessages(ctx, accountID, refs)
	if err != nil {
		return err
	}
	if err := s.attachMessageChanges(ctx, accountID, refs, messages, true); err != nil {
		return err
	}
	for _, contact := range contacts {
		var latest *waappv1.AccountMessage
		aliases := wamodel.ContactMessageRefs(contact)
		for _, message := range messages {
			if slices.Contains(aliases, message.GetContactRef()) && (latest == nil || shared.TimeFromProto(message.GetReceivedAt()).After(shared.TimeFromProto(latest.GetReceivedAt()))) {
				latest = message
			}
		}
		if latest == nil {
			continue
		}
		if latest.GetRevokedAt() != nil {
			contact.LastMessagePreview = "这条消息已撤回"
		} else {
			contact.LastMessagePreview = wamodel.ContactMessagePreview(latest.GetText().GetValue(), latest.GetText().GetRedactedValue(), "", latest.GetEncryptionState())
		}
	}
	return nil
}

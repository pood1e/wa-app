package wamodel

import (
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func MergeWAContact(existing, next *waappv1.WAContact) *waappv1.WAContact {
	next = NormalizedWAContactForStorage(next)
	if next == nil || existing == nil {
		return next
	}
	next.Number = shared.FirstNonEmpty(next.GetNumber(), existing.GetNumber())
	next.WaName = shared.FirstNonEmpty(next.GetWaName(), existing.GetWaName())
	next.VerifiedName = shared.FirstNonEmpty(next.GetVerifiedName(), existing.GetVerifiedName())
	next.ProfilePictureId = shared.FirstNonEmpty(next.GetProfilePictureId(), existing.GetProfilePictureId())
	next.SavedName = shared.FirstNonEmpty(next.GetSavedName(), existing.GetSavedName())
	if next.GetDisplayName() == "" || (existing.GetDisplayName() != "" && !ContactNameNeedsResolution(existing.GetDisplayName(), existing.GetNumber()) && next.GetKind() != waappv1.WAContactKind_WA_CONTACT_KIND_GROUP) {
		next.DisplayName = existing.GetDisplayName()
	}
	if next.GetSavedName() != "" {
		next.DisplayName = next.GetSavedName()
	}
	if existing.GetKind() != waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED && (next.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED || next.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_USER) {
		next.Kind = existing.GetKind()
	}
	next.IsWhatsappUser = next.GetIsWhatsappUser() || existing.GetIsWhatsappUser()
	next.IsReachable = next.GetIsReachable() || existing.GetIsReachable()
	if next.Audit == nil {
		next.Audit = existing.GetAudit()
	} else if existing.GetAudit() != nil {
		next.Audit.CreatedAt = existing.GetAudit().GetCreatedAt()
		if next.GetAudit().GetUpdatedAt().AsTime().Before(existing.GetAudit().GetUpdatedAt().AsTime()) {
			next.Audit.UpdatedAt = existing.GetAudit().GetUpdatedAt()
		}
	}
	return next
}

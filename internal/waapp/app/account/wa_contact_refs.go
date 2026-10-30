package account

import (
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func contactActionRefs(contactRef string, contact *waappv1.WAContact) []string {
	refs := wamodel.ContactRefVariants(contactRef)
	if contact != nil {
		refs = append(refs, contact.GetContactId())
		refs = append(refs, wamodel.ContactMessageRefs(contact)...)
	}
	return shared.UniqueNonEmptyStrings(refs...)
}

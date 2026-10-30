package wamodel

import (
	"strings"
	"unicode/utf8"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ContactMessageRefs(contact *waappv1.WAContact) []string {
	if contact == nil {
		return nil
	}
	refs := ContactRefVariants(contact.GetJid())
	refs = append(refs, ContactRefVariants(contact.GetNumber())...)
	return shared.UniqueNonEmptyStrings(refs...)
}

func ContactRefVariants(contactRef string) []string {
	contactRef = strings.TrimSpace(contactRef)
	numberRef := strings.TrimPrefix(contactRef, "+")
	if local, domain, ok := strings.Cut(numberRef, "@"); ok && domain == "s.whatsapp.net" {
		numberRef = local
	} else if strings.Contains(numberRef, "@") {
		numberRef = contactRef
	}
	return shared.UniqueNonEmptyStrings(contactRef, numberRef, wacore.NormalizeWAJID(numberRef))
}

func ContactKindStorageValue(contact *waappv1.WAContact) string {
	if contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED {
		return waappv1.WAContactKind_WA_CONTACT_KIND_USER.String()
	}
	return contact.GetKind().String()
}

func EnrichWAContactFallback(contact *waappv1.WAContact) {
	if contact == nil {
		return
	}
	NormalizeWAContactNames(contact)
	if contact.GetSavedName() != "" {
		return
	}
	contact.DisplayName = StoredWAContactDisplayName(contact.GetDisplayName(), contact.GetNumber())
	if contact.GetDisplayName() != "" {
		return
	}
	contact.DisplayName = FallbackWAContactDisplayName(contact.GetKind(), contact.GetJid(), contact.GetNumber())
}

func StoredWAContactDisplayName(value string, number string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if ContactNameNeedsResolution(value, number) {
		return ""
	}
	return value
}

func FallbackWAContactDisplayName(kind waappv1.WAContactKind, jid string, number string) string {
	if number != "" {
		return "+" + number
	}
	local, _, _ := strings.Cut(jid, "@")
	local = strings.TrimSpace(local)
	switch kind {
	case waappv1.WAContactKind_WA_CONTACT_KIND_GROUP:
		return FallbackContactName("群组", local)
	case waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS:
		return FallbackContactName("联系人", local)
	case waappv1.WAContactKind_WA_CONTACT_KIND_INTEROP:
		return FallbackContactName("互通联系人", local)
	case waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM:
		if jid == "status@broadcast" {
			return "状态"
		}
		return FallbackContactName("系统联系人", local)
	case waappv1.WAContactKind_WA_CONTACT_KIND_USER:
		if strings.HasSuffix(jid, "@lid") {
			return "未知联系人"
		}
		return FallbackContactName("联系人", local)
	default:
		return FallbackContactName("联系人", local)
	}
}

func FallbackContactName(prefix string, value string) string {
	value = ShortContactToken(value)
	if value == "" {
		return prefix
	}
	return prefix + " " + value
}

func ShortContactToken(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= 12 {
		return value
	}
	runes := []rune(value)
	return string(runes[:8]) + "…" + string(runes[len(runes)-4:])
}

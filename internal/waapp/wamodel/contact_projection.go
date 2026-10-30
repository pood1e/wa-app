package wamodel

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// ContactsFromInboundMessages projects the distinct WA contacts referenced by a
// batch of inbound messages, keeping the most recently seen record per JID.
func ContactsFromInboundMessages(accountID string, messages []*waappv1.InboundMessage, now time.Time) []*waappv1.WAContact {
	contacts := map[string]*waappv1.WAContact{}
	for _, msg := range messages {
		if msg.GetKind() != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE {
			continue
		}
		contactRef := ContactRefForMessage(msg.GetContactRef(), msg.GetSenderRef())
		contact := ContactFromRef(accountID, contactRef, shared.TimeFromProto(msg.GetReceivedAt()), now)
		if contact == nil {
			continue
		}
		current := contacts[contact.GetJid()]
		if current == nil || shared.TimeFromProto(contact.GetAudit().GetUpdatedAt()).After(shared.TimeFromProto(current.GetAudit().GetUpdatedAt())) {
			contacts[contact.GetJid()] = contact
		}
	}
	out := make([]*waappv1.WAContact, 0, len(contacts))
	for _, contact := range contacts {
		out = append(out, contact)
	}
	return out
}

// ContactFromRef builds a WA contact from a bare JID/reference.
func ContactFromRef(accountID string, ref string, seenAt time.Time, now time.Time) *waappv1.WAContact {
	jid := wacore.NormalizeWAJID(ref)
	if accountID == "" || jid == "" || jid == "unknown" {
		return nil
	}
	kind := contactKindForJID(jid)
	updatedAt := seenAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	return &waappv1.WAContact{
		ContactId:      "wact_" + shared.StableID(accountID+":"+jid),
		WaAccountId:    accountID,
		Jid:            jid,
		Number:         ContactNumberForJID(jid),
		DisplayName:    FallbackWAContactDisplayName(kind, jid, ContactNumberForJID(jid)),
		Kind:           kind,
		IsWhatsappUser: kind == waappv1.WAContactKind_WA_CONTACT_KIND_USER || kind == waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS || kind == waappv1.WAContactKind_WA_CONTACT_KIND_GROUP,
		IsReachable:    kind == waappv1.WAContactKind_WA_CONTACT_KIND_USER || kind == waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS || kind == waappv1.WAContactKind_WA_CONTACT_KIND_GROUP,
		Audit:          &waappv1.AuditStamp{CreatedAt: timestamppb.New(updatedAt.UTC()), UpdatedAt: timestamppb.New(updatedAt.UTC())},
	}
}

// ContactFromDecryptedMessage infers a contact (display name/kind) from the
// decrypted body of an inbound message.
func ContactFromDecryptedMessage(accountID string, msg *waappv1.InboundMessage, text string, now time.Time) *waappv1.WAContact {
	if msg == nil || msg.GetKind() != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE {
		return nil
	}
	contact := ContactFromRef(accountID, ContactRefForMessage(msg.GetContactRef(), msg.GetSenderRef()), shared.TimeFromProto(msg.GetReceivedAt()), now)
	if contact == nil {
		return nil
	}
	name, business := inferWAContactDisplayName(text, contact.GetJid())
	if name == "" {
		return nil
	}
	contact.DisplayName = name
	if business {
		contact.Kind = waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS
	}
	return contact
}

// ContactsFromContactHints projects contacts carried by a message's contact hints.
func ContactsFromContactHints(accountID string, msg *waappv1.InboundMessage, hints []wacore.WAContactHint, now time.Time) []*waappv1.WAContact {
	if accountID == "" || len(hints) == 0 {
		return nil
	}
	seenAt := now
	if msg != nil {
		if receivedAt := shared.TimeFromProto(msg.GetReceivedAt()); !receivedAt.IsZero() {
			seenAt = receivedAt
		}
	}
	contacts := []*waappv1.WAContact{}
	for _, hint := range hints {
		contact := contactFromContactHint(accountID, hint, seenAt, now)
		if contact == nil {
			continue
		}
		contacts = append(contacts, contact)
	}
	return DedupeWAContacts(contacts)
}

func contactFromContactHint(accountID string, hint wacore.WAContactHint, seenAt time.Time, now time.Time) *waappv1.WAContact {
	hint = hint.Normalized()
	if !hint.Valid() {
		return nil
	}
	contact := ContactFromRef(accountID, hint.LIDJID, seenAt, now)
	if contact == nil {
		return nil
	}
	contact.Number = ContactNumberForJID(hint.PNJID)
	contact.DisplayName = shared.FirstNonEmpty(hint.DisplayName, hint.VerifiedName, hint.WAName, hint.Username, FallbackWAContactDisplayName(contact.GetKind(), contact.GetJid(), contact.GetNumber()))
	contact.WaName = shared.FirstNonEmpty(hint.WAName, hint.Username)
	contact.VerifiedName = hint.VerifiedName
	if hint.VerifiedName != "" {
		contact.Kind = waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS
	}
	NormalizeWAContactNames(contact)
	return contact
}

// ContactNumberForJID extracts the E.164-style number from a phone JID.
func ContactNumberForJID(jid string) string {
	local, domain, ok := strings.Cut(jid, "@")
	if !ok || domain != "s.whatsapp.net" {
		return ""
	}
	return shared.DigitsOnly(local)
}

func contactKindForJID(jid string) waappv1.WAContactKind {
	switch {
	case jid == "status@broadcast" || strings.HasSuffix(jid, "@broadcast"):
		return waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM
	case strings.HasSuffix(jid, "@g.us"):
		return waappv1.WAContactKind_WA_CONTACT_KIND_GROUP
	case strings.HasSuffix(jid, "@lid"), strings.HasSuffix(jid, "@s.whatsapp.net"):
		return waappv1.WAContactKind_WA_CONTACT_KIND_USER
	case strings.Contains(jid, "interop"):
		return waappv1.WAContactKind_WA_CONTACT_KIND_INTEROP
	default:
		return waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM
	}
}

func inferWAContactDisplayName(text string, jid string) (string, bool) {
	value := strings.ToLower(text)
	switch {
	case strings.Contains(value, "facebook.com") || strings.Contains(value, " facebook"):
		return "Facebook", true
	case strings.Contains(value, "instagram.com") || strings.Contains(value, " instagram"):
		return "Instagram", true
	case strings.HasSuffix(wacore.NormalizeWAJID(jid), "@lid") && looksLikeVerificationCodeOnlyMessage(text):
		return "验证码服务", true
	default:
		return "", false
	}
}

func looksLikeVerificationCodeOnlyMessage(text string) bool {
	value := strings.TrimSpace(text)
	if value == "" || utf8.RuneCountInString(value) > 32 {
		return false
	}
	digits := shared.DigitsOnly(value)
	if len(digits) < 4 || len(digits) > 10 {
		return false
	}
	for _, r := range value {
		if unicode.IsDigit(r) || unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '-', '–', '—', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}

// DedupeWAContacts merges contacts sharing a JID, preferring the richer field values.
func DedupeWAContacts(contacts []*waappv1.WAContact) []*waappv1.WAContact {
	if len(contacts) == 0 {
		return nil
	}
	merged := map[string]*waappv1.WAContact{}
	order := []string{}
	for _, contact := range contacts {
		if contact == nil || contact.GetJid() == "" {
			continue
		}
		key := contact.GetJid()
		current := merged[key]
		if current == nil {
			merged[key] = contact
			order = append(order, key)
			continue
		}
		current.Number = shared.FirstNonEmpty(current.GetNumber(), contact.GetNumber())
		current.DisplayName = betterWAContactDisplayName(current, contact.GetDisplayName())
		current.WaName = shared.FirstNonEmpty(current.GetWaName(), contact.GetWaName())
		current.VerifiedName = shared.FirstNonEmpty(current.GetVerifiedName(), contact.GetVerifiedName())
		current.ProfilePictureId = shared.FirstNonEmpty(current.GetProfilePictureId(), contact.GetProfilePictureId())
		if current.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_USER && contact.GetKind() != waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED {
			current.Kind = contact.GetKind()
		}
		current.IsWhatsappUser = current.GetIsWhatsappUser() || contact.GetIsWhatsappUser()
		current.IsReachable = current.GetIsReachable() || contact.GetIsReachable()
	}
	out := make([]*waappv1.WAContact, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}

func betterWAContactDisplayName(contact *waappv1.WAContact, candidate string) string {
	candidate = wacore.WAContactName(candidate)
	if candidate == "" {
		return contact.GetDisplayName()
	}
	current := contact.GetDisplayName()
	if ContactNameNeedsResolution(current, contact.GetNumber()) || ContactDisplayNeedsResolution(contact) {
		return candidate
	}
	return current
}

func ContactDisplayNeedsResolution(contact *waappv1.WAContact) bool {
	if contact == nil {
		return false
	}
	name := strings.TrimSpace(contact.GetDisplayName())
	return ContactNameNeedsResolution(name, contact.GetNumber())
}

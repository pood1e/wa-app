package communication

import (
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func ContactsFromContactUsyncList(accountID string, listNode pwire.ChatdNode, now time.Time, requestedJIDs []string) []*waappv1.WAContact {
	contacts := []*waappv1.WAContact{}
	for index, userNode := range pwire.ChatdChildren(listNode) {
		if userNode.Tag != "user" {
			continue
		}
		fallbackLID := ""
		if index < len(requestedJIDs) {
			fallbackLID = requestedJIDs[index]
		}
		if contact := ContactFromContactUsyncUser(accountID, userNode, now, fallbackLID); contact != nil {
			contacts = append(contacts, contact)
		}
	}
	return contacts
}

func ContactFromContactUsyncUser(accountID string, userNode pwire.ChatdNode, now time.Time, fallbackLID string) *waappv1.WAContact {
	jid := wacore.NormalizeWAJID(userNode.Attrs["jid"])
	contactNode, _ := ChatdChild(userNode, "contact")
	if contactNode.Attrs["type"] == "out" || contactNode.Attrs["type"] == "invalid" {
		return nil
	}
	pnJID := wacore.NormalizeWAJID(shared.FirstNonEmpty(
		userNode.Attrs["pn_jid"],
		userNode.Attrs["new_jid"],
		contactNode.Attrs["pn_jid"],
		contactNode.Attrs["jid"],
		pwire.PhoneNumberWAJID(ChatdNodeText(contactNode)),
		FirstPNJIDInNode(userNode),
	))
	lidJID := ""
	switch {
	case strings.HasSuffix(jid, "@lid"):
		lidJID = jid
	case strings.HasSuffix(jid, "@s.whatsapp.net"):
		pnJID = shared.FirstNonEmpty(pnJID, jid)
	}
	if lidNode, ok := ChatdChild(userNode, "lid"); ok {
		lidJID = shared.FirstNonEmpty(lidJID, contactUsyncLID(shared.FirstNonEmpty(lidNode.Attrs["val"], lidNode.Attrs["jid"], ChatdNodeText(lidNode))))
	}
	if businessNode, ok := ChatdChild(userNode, "business"); ok {
		pnJID = shared.FirstNonEmpty(pnJID, wacore.NormalizeWAJID(businessNode.Attrs["pn_jid"]), FirstPNJIDInNode(businessNode))
	}
	lidJID = shared.FirstNonEmpty(lidJID, FirstLIDJIDInNode(userNode), NormalizeContactUsyncFallbackLID(fallbackLID))
	if lidJID == "" && (pnJID == "" || contactNode.Attrs["type"] != "in") {
		return nil
	}
	contact := wamodel.ContactFromRef(accountID, shared.FirstNonEmpty(lidJID, pnJID), now, now)
	if contact == nil {
		return nil
	}
	contact.Number = shared.FirstNonEmpty(wamodel.ContactNumberForJID(pnJID), ContactUsyncPhoneNumber(userNode))
	displayName, waName, verifiedName, business := ContactUsyncNames(userNode)
	contact.DisplayName = shared.FirstNonEmpty(displayName, verifiedName, waName, wamodel.FallbackWAContactDisplayName(contact.GetKind(), contact.GetJid(), contact.GetNumber()))
	contact.WaName = waName
	contact.VerifiedName = verifiedName
	contact.ProfilePictureId = ContactProfilePictureID(userNode)
	if business || verifiedName != "" {
		contact.Kind = waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS
	}
	wamodel.NormalizeWAContactNames(contact)
	return contact
}

func contactUsyncLID(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && shared.DigitsOnly(value) == value {
		return value + "@lid"
	}
	return NormalizeContactUsyncFallbackLID(value)
}

func NormalizeContactUsyncFallbackLID(jid string) string {
	jid = wacore.NormalizeWAJID(jid)
	if strings.HasSuffix(jid, "@lid") {
		return jid
	}
	return ""
}

func ContactUsyncNames(userNode pwire.ChatdNode) (string, string, string, bool) {
	contactNode, _ := ChatdChild(userNode, "contact")
	usernameNode, hasUsername := ChatdChild(userNode, "username")
	businessNode, hasBusiness := ChatdChild(userNode, "business")
	businessProfileNode, hasBusinessProfile := FindChatdNode(userNode, "business_profile")
	displayName := wacore.WAContactName(shared.FirstNonEmpty(
		userNode.Attrs["display_name"],
		userNode.Attrs["notify"],
		contactNode.Attrs["display_name"],
		contactNode.Attrs["name"],
		contactNode.Attrs["notify"],
		ContactTextName(contactNode),
	))
	username := ""
	if hasUsername {
		username = wacore.WAContactName(shared.FirstNonEmpty(FirstUsernameInNode(usernameNode), ChatdNodeText(usernameNode), usernameNode.Attrs["username"], usernameNode.Attrs["value"], usernameNode.Attrs["id"]))
	}
	username = wacore.WAContactName(shared.FirstNonEmpty(username, FirstUsernameInNode(userNode), userNode.Attrs["username"], contactNode.Attrs["username"]))
	verifiedName := ""
	if hasBusiness {
		if verifiedNode, ok := ChatdChild(businessNode, "verified_name"); ok {
			verifiedName = shared.FirstNonEmpty(verifiedName, BusinessNodeName(verifiedNode))
		}
		if profileNode, ok := ChatdChild(businessNode, "profile"); ok {
			verifiedName = shared.FirstNonEmpty(verifiedName, BusinessVerifiedNodeName(profileNode))
			displayName = shared.FirstNonEmpty(displayName, BusinessNodeName(profileNode))
		}
	}
	if hasBusinessProfile {
		displayName = shared.FirstNonEmpty(displayName, BusinessNodeName(businessProfileNode), BusinessProfileNodeName(businessProfileNode))
		verifiedName = shared.FirstNonEmpty(verifiedName, BusinessVerifiedNodeName(businessProfileNode))
	}
	return displayName, username, verifiedName, hasBusiness || hasBusinessProfile
}

func FirstUsernameInNode(node pwire.ChatdNode) string {
	for _, key := range []string{"username", "value", "id"} {
		if value := pwire.WaContactUsername(node.Attrs[key]); value != "" {
			return value
		}
	}
	if node.Tag == "username" || node.Tag == "username_info" {
		if value := pwire.WaContactUsername(ChatdNodeText(node)); value != "" {
			return value
		}
	}
	for _, child := range pwire.ChatdChildren(node) {
		if value := FirstUsernameInNode(child); value != "" {
			return value
		}
	}
	return ""
}

func ContactTextName(node pwire.ChatdNode) string {
	text := wacore.WAContactName(ChatdNodeText(node))
	switch strings.ToLower(text) {
	case "", "in", "out", "invalid":
		return ""
	default:
		return text
	}
}

func FirstPNJIDInNode(node pwire.ChatdNode) string {
	return FirstJIDInNodeBySuffix(node, "@s.whatsapp.net", AllowedPNJIDAttr)
}

func FirstLIDJIDInNode(node pwire.ChatdNode) string {
	return FirstJIDInNodeBySuffix(node, "@lid", AllowedLIDJIDAttr)
}

func FirstJIDInNodeBySuffix(node pwire.ChatdNode, suffix string, allowed func(string, string) bool) string {
	for key, value := range node.Attrs {
		if !allowed(node.Tag, key) {
			continue
		}
		jid := JidFromChatdValue(key, value)
		if strings.HasSuffix(jid, suffix) {
			return jid
		}
	}
	if allowed(node.Tag, "") {
		text := JidFromChatdValue(node.Tag, ChatdNodeText(node))
		if strings.HasSuffix(text, suffix) {
			return text
		}
	}
	for _, child := range pwire.ChatdChildren(node) {
		if jid := FirstJIDInNodeBySuffix(child, suffix, allowed); jid != "" {
			return jid
		}
	}
	return ""
}

func JidFromChatdValue(key string, value string) string {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	jid := wacore.NormalizeWAJID(value)
	if strings.Contains(value, "@") {
		return jid
	}
	switch key {
	case "number", "phone", "phone_number", "business_phone_number", "wa_id", "pn":
		return pwire.PhoneNumberWAJID(value)
	default:
		return jid
	}
}

func AllowedPNJIDAttr(tag string, key string) bool {
	switch key {
	case "jid", "pn", "pn_jid", "new_jid", "sender_pn", "sender_pn_jid",
		"participant_pn", "participant_pn_jid", "peer_recipient_pn", "peer_recipient_pn_jid",
		"recipient_pn", "recipient_pn_jid", "contact_pn", "contact_pn_jid",
		"author_pn", "author_pn_jid", "creator_pn", "creator_pn_jid", "phone_number",
		"business_phone_number", "number", "phone", "wa_id":
		return true
	default:
		return key == "" && (tag == "phone_number" || tag == "business_phone_number" || tag == "number" || tag == "phone")
	}
}

func AllowedLIDJIDAttr(tag string, key string) bool {
	switch key {
	case "jid", "lid", "lid_jid", "new_lid", "sender_lid", "participant_lid",
		"peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "contact_lid",
		"author_lid", "creator_lid":
		return true
	default:
		return key == "" && (tag == "lid" || tag == "lid_jid")
	}
}

func BusinessNodeName(node pwire.ChatdNode) string {
	for _, key := range []string{"verified_name", "business_name", "display_name", "name", "push_name", "title"} {
		if value := wacore.WAContactName(node.Attrs[key]); value != "" {
			return value
		}
	}
	switch node.Tag {
	case "verified_name", "business_name", "display_name", "name", "push_name", "title":
		if value := wacore.WAContactName(ChatdNodeText(node)); value != "" {
			return value
		}
	}
	for _, child := range pwire.ChatdChildren(node) {
		if value := BusinessNodeName(child); value != "" {
			return value
		}
	}
	return ""
}

func BusinessProfileNodeName(node pwire.ChatdNode) string {
	for _, tag := range []string{"business_name", "verified_name", "biz_identity_info", "business_identity_info", "display_name", "name", "push_name", "profile_name"} {
		if child, ok := FindChatdNode(node, tag); ok {
			if value := BusinessNodeName(child); value != "" {
				return value
			}
		}
	}
	return ""
}

package communication

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func BusinessVerifiedNodeName(node pwire.ChatdNode) string {
	for _, tag := range []string{"verified_name", "biz_identity_info", "business_identity_info"} {
		if child, ok := FindChatdNode(node, tag); ok {
			if value := BusinessNodeName(child); value != "" {
				return value
			}
		}
	}
	return ""
}

func ContactUsyncPhoneNumber(node pwire.ChatdNode) string {
	for _, key := range []string{"number", "phone", "phone_number", "business_phone_number", "pn", "wa_id"} {
		if number := shared.DigitsOnly(node.Attrs[key]); number != "" {
			return number
		}
	}
	switch node.Tag {
	case "number", "phone", "phone_number", "business_phone_number":
		if number := shared.DigitsOnly(ChatdNodeText(node)); number != "" {
			return number
		}
	}
	for _, child := range pwire.ChatdChildren(node) {
		if number := ContactUsyncPhoneNumber(child); number != "" {
			return number
		}
	}
	return ""
}

func ContactProfilePictureID(node pwire.ChatdNode) string {
	pictureNode, ok := FindChatdNode(node, "picture")
	if !ok {
		return ""
	}
	return strings.TrimSpace(shared.FirstNonEmpty(pictureNode.Attrs["id"], pictureNode.Attrs["photo_id"], pictureNode.Attrs["picture_id"]))
}

func NormalizeContactUsyncJIDs(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		jid := wacore.NormalizeWAJID(value)
		if !strings.HasSuffix(jid, "@lid") && !strings.HasSuffix(jid, "@s.whatsapp.net") {
			continue
		}
		if _, ok := seen[jid]; ok {
			continue
		}
		seen[jid] = struct{}{}
		out = append(out, jid)
	}
	return out
}

func FindChatdNode(node pwire.ChatdNode, tag string) (pwire.ChatdNode, bool) {
	if node.Tag == tag {
		return node, true
	}
	for _, child := range pwire.ChatdChildren(node) {
		if found, ok := FindChatdNode(child, tag); ok {
			return found, true
		}
	}
	return pwire.ChatdNode{}, false
}

func ContactUsyncIdentityCount(contacts []*waappv1.WAContact) int {
	count := 0
	for _, contact := range contacts {
		if ContactUsyncHasIdentity(contact) {
			count++
		}
	}
	return count
}

func ContactUsyncHasIdentity(contact *waappv1.WAContact) bool {
	if contact == nil {
		return false
	}
	if contact.GetNumber() != "" || contact.GetWaName() != "" || contact.GetVerifiedName() != "" {
		return true
	}
	name := strings.TrimSpace(contact.GetDisplayName())
	return name != "" && name != "未知联系人" && !strings.HasPrefix(name, "联系人 ") && !strings.HasPrefix(name, "LID ")
}

func ContactUsyncHasDisplayIdentity(contact *waappv1.WAContact) bool {
	if contact == nil {
		return false
	}
	if wamodel.ResolvedWAContactName(contact.GetWaName(), contact.GetNumber()) != "" || wamodel.ResolvedWAContactName(contact.GetVerifiedName(), contact.GetNumber()) != "" {
		return true
	}
	return !wamodel.ContactDisplayNeedsResolution(contact)
}

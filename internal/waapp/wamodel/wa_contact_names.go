package wamodel

import (
	"strings"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type WAKnownContactAlias struct {
	Name        string
	JIDs        []string
	Numbers     []string
	BusinessIDs []string
}

var WAKnownContactAliases = []WAKnownContactAlias{
	{
		Name:        "OpenAI",
		JIDs:        []string{"227775403311132@lid"},
		Numbers:     []string{"18668392077"},
		BusinessIDs: []string{"1191555928498480"},
	},
}

func NormalizedWAContactForStorage(contact *waappv1.WAContact) *waappv1.WAContact {
	if contact == nil {
		return nil
	}
	clone := proto.Clone(contact).(*waappv1.WAContact)
	NormalizeWAContactNames(clone)
	return clone
}

func NormalizeWAContactNames(contact *waappv1.WAContact) {
	if contact == nil {
		return
	}
	alias := KnownWAContactAliasName(contact)
	contact.DisplayName = ResolvedWAContactName(contact.GetDisplayName(), contact.GetNumber())
	contact.WaName = ResolvedWAContactName(contact.GetWaName(), contact.GetNumber())
	contact.VerifiedName = ResolvedWAContactName(contact.GetVerifiedName(), contact.GetNumber())
	contact.SavedName = wacore.WAContactName(contact.GetSavedName())
	if contact.GetSavedName() != "" {
		contact.DisplayName = contact.GetSavedName()
		return
	}
	if contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_GROUP && contact.GetDisplayName() == FallbackWAContactDisplayName(contact.GetKind(), contact.GetJid(), contact.GetNumber()) {
		contact.DisplayName = ""
	}
	if alias == "" {
		return
	}
	contact.DisplayName = alias
	if contact.GetWaName() == "" {
		contact.WaName = alias
	}
	contact.Kind = waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS
}

func KnownWAContactAliasName(contact *waappv1.WAContact) string {
	if contact == nil {
		return ""
	}
	jid := wacore.NormalizeWAJID(contact.GetJid())
	number := shared.DigitsOnly(contact.GetNumber())
	businessIDs := shared.UniqueNonEmptyStrings(shared.DigitsOnly(contact.GetDisplayName()), shared.DigitsOnly(contact.GetWaName()), shared.DigitsOnly(contact.GetVerifiedName()))
	for _, alias := range WAKnownContactAliases {
		if StringInSlice(jid, alias.JIDs) || StringInSlice(number, alias.Numbers) {
			return alias.Name
		}
		for _, businessID := range businessIDs {
			if StringInSlice(businessID, alias.BusinessIDs) {
				return alias.Name
			}
		}
	}
	return ""
}

func ResolvedWAContactName(value string, number string) string {
	name := wacore.WAContactName(value)
	if ContactNameNeedsResolution(name, number) {
		return ""
	}
	return name
}

func ContactNameNeedsResolution(name string, number string) bool {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return true
	case name == "0" || name == "未知联系人":
		return true
	case strings.HasPrefix(name, "联系人 ") || strings.HasPrefix(name, "LID ") || strings.HasPrefix(name, "企业账号 "):
		return true
	case IsNumericWAContactName(name):
		return true
	}
	number = shared.DigitsOnly(number)
	return number != "" && (name == "+"+number || shared.DigitsOnly(name) == number)
}

func IsNumericWAContactName(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "+")
	if len(value) < 6 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func StringInSlice(value string, values []string) bool {
	if value == "" {
		return false
	}
	for _, item := range values {
		if value == item {
			return true
		}
	}
	return false
}

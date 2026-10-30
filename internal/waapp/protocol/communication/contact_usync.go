package communication

import (
	"errors"
	"slices"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

// BuildContactUsyncIQ follows Android 2.26.40.76 X.0qx.A06 and X.1B3 ADD_QUERY / INTERACTIVE_QUERY.
func BuildContactUsyncIQ(id, sid string, jids []string) pwire.ChatdNode {
	context := "interactive"
	users := make([]pwire.ChatdNode, 0, len(jids))
	for _, jid := range jids {
		user := pwire.ChatdNode{Tag: "user"}
		if strings.HasSuffix(jid, "@s.whatsapp.net") {
			context = "add"
			number := strings.TrimSuffix(jid, "@s.whatsapp.net")
			user.Content = []pwire.ChatdNode{{Tag: "contact", Content: "+" + number}}
		} else {
			user.Attrs = map[string]string{"jid": jid}
		}
		users = append(users, user)
	}
	query := []pwire.ChatdNode{
		{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
		{Tag: "lid"},
	}
	if context == "interactive" {
		query = append(query,
			pwire.ChatdNode{Tag: "username"},
			pwire.ChatdNode{Tag: "picture", Attrs: map[string]string{"type": "preview"}},
			pwire.ChatdNode{Tag: "business", Content: []pwire.ChatdNode{
				{Tag: "verified_name"}, {Tag: "profile", Attrs: map[string]string{"v": "1"}},
			}},
		)
	}
	return pwire.ChatdNode{
		Tag: "iq", Attrs: map[string]string{"xmlns": "usync", "id": id, "type": "get"},
		Content: []pwire.ChatdNode{{Tag: "usync", Attrs: map[string]string{
			"sid": sid, "index": "0", "last": "true", "mode": "query", "context": context,
		}, Content: []pwire.ChatdNode{{Tag: "query", Content: query}, {Tag: "list", Content: users}}}},
	}
}

func contactUsyncBatches(jids []string) [][]string {
	var phones, lids []string
	for _, jid := range jids {
		if strings.HasSuffix(jid, "@s.whatsapp.net") {
			phones = append(phones, jid)
		} else {
			lids = append(lids, jid)
		}
	}
	var batches [][]string
	for _, group := range [][]string{phones, lids} {
		for batch := range slices.Chunk(group, MaxContactUsyncBatchSize) {
			batches = append(batches, batch)
		}
	}
	return batches
}

func ContactsFromContactUsyncIQ(accountID string, response pwire.ChatdNode, now time.Time, requestedJIDs []string) []*waappv1.WAContact {
	if accountID == "" {
		return nil
	}
	usync, ok := FindChatdNode(response, "usync")
	if !ok {
		return nil
	}
	contacts := []*waappv1.WAContact{}
	for _, listTag := range []string{"list", "side_list"} {
		if listNode, ok := ChatdChild(usync, listTag); ok {
			contacts = append(contacts, ContactsFromContactUsyncList(accountID, listNode, now, requestedJIDs)...)
		}
	}
	return wamodel.DedupeWAContacts(contacts)
}

func validateContactUsyncResponse(response pwire.ChatdNode, requested []string) error {
	if err := ChatdIQError(response); err != nil {
		return err
	}
	usync, ok := ChatdChild(response, "usync")
	if !ok {
		return errors.New("contact response is missing usync")
	}
	list, ok := ChatdChild(usync, "list")
	if !ok {
		return errors.New("contact response is missing the user list")
	}
	if len(requested) == 0 || !strings.HasSuffix(requested[0], "@s.whatsapp.net") {
		return nil
	}
	users := pwire.ChatdChildren(list)
	if len(users) != len(requested) {
		return errors.New("contact lookup returned an incomplete user list")
	}
	for _, user := range users {
		contact, ok := ChatdChild(user, "contact")
		if user.Tag != "user" || !ok {
			return errors.New("contact lookup returned an incomplete user result")
		}
		switch contact.Attrs["type"] {
		case "in", "out", "invalid":
		default:
			return errors.New("contact lookup returned an unknown registration status")
		}
	}
	return nil
}

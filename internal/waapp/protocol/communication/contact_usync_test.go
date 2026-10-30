package communication

import (
	"context"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type contactQuerySender struct {
	requests []wire.ChatdNode
	response wire.ChatdNode
	err      error
}

func (s *contactQuerySender) SendIQ(_ context.Context, _ state.NativeState, _, _ string, request wire.ChatdNode, _ string) (wire.ChatdNode, ChatdSessionUpdate, error) {
	s.requests = append(s.requests, request)
	return s.response, ChatdSessionUpdate{}, s.err
}

func contactLookupResponse(kind string) wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}, Content: []wire.ChatdNode{{
		Tag: "usync", Content: []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{{
			Tag: "user", Attrs: map[string]string{"jid": "12025550124@s.whatsapp.net"}, Content: []wire.ChatdNode{
				{Tag: "contact", Attrs: map[string]string{"type": kind}, Content: "+12025550124"},
				{Tag: "lid", Attrs: map[string]string{"val": "100001@lid"}},
			},
		}}}},
	}}}
}

func TestNativeContactLookupReturnsCanonicalIdentity(t *testing.T) {
	engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
	sender := &contactQuerySender{response: contactLookupResponse("in")}
	input := wacore.EngineContactResolveInput{WAAccountID: "account", ClientProfileID: "current", JIDs: []string{"12025550124@s.whatsapp.net"}}
	result := engine.ResolveContactsWithSender(t.Context(), input, sender)
	if result.Err != nil || len(result.Contacts) != 1 || result.Contacts[0].GetJid() != "100001@lid" || result.Contacts[0].GetNumber() != "12025550124" {
		t.Fatalf("native lookup did not retain the phone-to-LID mapping: %v", result.Err)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("lookup sent %d queries, want 1", len(sender.requests))
	}
	usync, _ := ChatdChild(sender.requests[0], "usync")
	list, _ := ChatdChild(usync, "list")
	users := wire.ChatdChildren(list)
	phone, _ := ChatdChild(users[0], "contact")
	if usync.Attrs["context"] != "add" || usync.Attrs["mode"] != "query" || len(users[0].Attrs) != 0 || ChatdNodeText(phone) != "+12025550124" {
		t.Fatal("phone lookup did not use the native ADD_QUERY contact body")
	}
}

func TestContactLookupNeverCreatesUnregisteredContacts(t *testing.T) {
	for _, kind := range []string{"out", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			if contacts := ContactsFromContactUsyncIQ("account", contactLookupResponse(kind), time.Now(), []string{"12025550124@s.whatsapp.net"}); len(contacts) != 0 {
				t.Fatal("negative contact result was accepted as a WhatsApp user")
			}
		})
	}
}

func TestContactLookupPreservesPNOnlyResponses(t *testing.T) {
	response := contactLookupResponse("in")
	usync := wire.ChatdChildren(response)[0]
	list := wire.ChatdChildren(usync)[0]
	user := wire.ChatdChildren(list)[0]
	user.Content = wire.ChatdChildren(user)[:1]
	contacts := ContactsFromContactUsyncList("account", wire.ChatdNode{Tag: "list", Content: []wire.ChatdNode{user}}, time.Now(), nil)
	if len(contacts) != 1 || contacts[0].GetJid() != "12025550124@s.whatsapp.net" {
		t.Fatal("registered PN user without a LID was discarded")
	}
}

func TestContactLookupKeepsNumericLIDsInTheirNamespace(t *testing.T) {
	response := contactLookupResponse("in")
	usync := wire.ChatdChildren(response)[0]
	list := wire.ChatdChildren(usync)[0]
	user := wire.ChatdChildren(list)[0]
	wire.ChatdChildren(user)[1].Attrs["val"] = "100001"
	contacts := ContactsFromContactUsyncIQ("account", response, time.Now(), nil)
	if len(contacts) != 1 || contacts[0].GetJid() != "100001@lid" {
		t.Fatal("numeric LID was interpreted as a phone number")
	}
}

func TestContactResolutionStopsAfterTransportFailure(t *testing.T) {
	engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
	sender := &contactQuerySender{err: context.DeadlineExceeded}
	result := engine.ResolveContactsWithSender(t.Context(), wacore.EngineContactResolveInput{ClientProfileID: "current", JIDs: []string{"12025550124@s.whatsapp.net"}}, sender)
	if result.Err == nil || len(result.Contacts) != 0 || len(sender.requests) != 1 {
		t.Fatal("failed lookup hid its error or tried speculative request variants")
	}
	sender.requests = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sender.err = ctx.Err()
	result = engine.ResolveContactsWithSender(ctx, wacore.EngineContactResolveInput{ClientProfileID: "current", JIDs: []string{"12025550124@s.whatsapp.net"}}, sender)
	if result.Err == nil || len(sender.requests) != 0 {
		t.Fatal("cancelled contact lookup succeeded")
	}
}

func TestMalformedContactResponseIsNotAnUnregisteredNumber(t *testing.T) {
	for _, response := range []wire.ChatdNode{
		{Tag: "iq", Attrs: map[string]string{"type": "result"}},
		contactLookupResponse("pending"),
		{Tag: "iq", Content: []wire.ChatdNode{{Tag: "usync", Content: []wire.ChatdNode{{Tag: "list"}}}}},
	} {
		if err := validateContactUsyncResponse(response, []string{"12025550124@s.whatsapp.net"}); err == nil {
			t.Fatal("incomplete contact response was accepted as a registration result")
		}
	}
}

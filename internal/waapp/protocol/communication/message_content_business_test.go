package communication

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestBusinessMessageProjection(t *testing.T) {
	hydrated := &waE2E.TemplateMessage_HydratedFourRowTemplate{
		Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "OpenAI"},
		HydratedContentText: proto.String("Your verification code is 012345.\nKeep it private."),
		HydratedFooterText:  proto.String("Expires in 5 minutes."),
		TemplateID:          proto.String("private-template-id"),
		HydratedButtons: []*waE2E.HydratedTemplateButton{{
			HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{
				UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
					DisplayText: proto.String("Copy code"), URL: proto.String("https://example.invalid/private-token"),
				},
			},
		}},
	}
	template := &waE2E.TemplateMessage{HydratedTemplate: hydrated}
	templateText := "OpenAI\nYour verification code is 012345.\nKeep it private.\nExpires in 5 minutes."
	interactive := &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{Title: proto.String(" Account access "), Subtitle: proto.String("\tVerification\t")},
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("\nYour code is 012345.\nKeep it private.\n")},
		Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(" ")},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{MessageParamsJSON: proto.String(`{"private":"token"}`)},
		},
	}
	interactiveText := "Account access\nVerification\nYour code is 012345.\nKeep it private."
	for _, test := range []struct {
		name    string
		message *waE2E.Message
		text    string
	}{
		{"hydrated_template", &waE2E.Message{TemplateMessage: template}, templateText},
		{"hydrated_four_row", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
			Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: hydrated},
		}}, templateText},
		{"hydrated_precedence", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
			HydratedTemplate: hydrated,
			Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
				HydratedContentText: proto.String("alternate content"),
			}},
		}}, templateText},
		{"empty_hydrated_fallback", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
			HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{},
			Format:           &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: hydrated},
		}}, templateText},
		{"hydrated_hsm", &waE2E.Message{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{HydratedHsm: template}}, templateText},
		{"interactive_template", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
			Format: &waE2E.TemplateMessage_InteractiveMessageTemplate{InteractiveMessageTemplate: interactive},
		}}, interactiveText},
		{"interactive", &waE2E.Message{InteractiveMessage: interactive}, interactiveText},
		{"buttons", &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
			Header:      &waE2E.ButtonsMessage_Text{Text: "Sign in"},
			ContentText: proto.String("Your code is 012345."), FooterText: proto.String("Expires soon."),
			Buttons: []*waE2E.ButtonsMessage_Button{{ButtonID: proto.String("private-button-id")}},
		}}, "Sign in\nYour code is 012345.\nExpires soon."},
		{"list", &waE2E.Message{ListMessage: &waE2E.ListMessage{
			Title: proto.String("Choose an option"), Description: proto.String("Account options"), FooterText: proto.String("Help"),
		}}, "Choose an option\nAccount options\nHelp"},
		{"template_reply", &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedDisplayText: proto.String("Confirm"), SelectedID: proto.String("private-selection-id"),
		}}, "Confirm"},
		{"buttons_reply", &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			Response: &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Confirm"}, SelectedButtonID: proto.String("private-button-id"),
		}}, "Confirm"},
		{"list_reply", &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
			Title: proto.String("Selected option"), Description: proto.String("Description"),
			SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("private-row-id")},
		}}, "Selected option\nDescription"},
		{"interactive_reply", &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{Text: proto.String("Confirm")},
			InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
				NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{ParamsJSON: proto.String(`{"private":"token"}`)},
			},
		}}, "Confirm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := proto.Clone(test.message)
			text, content, kind := ProjectMessage(test.message, true)
			if text != test.text || content.GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT || kind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE {
				t.Fatalf("projection text=%q type=%s kind=%s", text, content.GetType(), kind)
			}
			raw, err := proto.Marshal(test.message)
			if err != nil {
				t.Fatal(err)
			}
			if got := NativePlaintextText(padSignalMessage(raw)); got != test.text {
				t.Fatalf("padded display=%q want=%q", got, test.text)
			}
			if !proto.Equal(before, test.message) {
				t.Fatal("projection mutated the business message")
			}
		})
	}
}

func TestBusinessMessageWithoutDisplayText(t *testing.T) {
	for _, message := range []*waE2E.Message{
		{TemplateMessage: &waE2E.TemplateMessage{}},
		{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{Params: []string{"private-parameter"}}},
		{ButtonsMessage: &waE2E.ButtonsMessage{}},
		{ListMessage: &waE2E.ListMessage{}},
		{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{SelectedID: proto.String("private-selection-id")}},
		{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{SelectedButtonID: proto.String("private-button-id")}},
		{ListResponseMessage: &waE2E.ListResponseMessage{SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("private-row-id")}}},
		{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{}},
		{InteractiveMessage: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String(" \n\t ")},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{MessageParamsJSON: proto.String(`{"private":"token"}`)},
			},
		}},
	} {
		text, content, kind := ProjectMessage(message, true)
		if text != "[暂不支持的消息]" || content.GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_UNSUPPORTED || kind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE {
			t.Fatalf("projection text=%q type=%s kind=%s", text, content.GetType(), kind)
		}
	}
}

func TestBusinessMessageDecryptionExtractsOTP(t *testing.T) {
	body := &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
		HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{HydratedContentText: proto.String("Your verification code is 012345.")},
	}}
	raw, err := proto.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	current := newMessageParty(t)
	frame := signalConversationFixture(t, current, nil, [][]byte{padSignalMessage(raw)})[0]
	message := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "synthetic-profile"}
	ApplyChatdReceiveState(&current, message, []wire.ChatdEncPayload{{Payload: frame.preKey, EncType: "pkmsg", Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	if err := engine.SaveState(t.Context(), message.ClientProfileID, current); err != nil {
		t.Fatal(err)
	}
	input := wacore.EngineDecryptInput{
		MessageID: "template-message", ClientProfileID: message.ClientProfileID,
		PayloadRef:          wire.PayloadRefForEnc(message.WAAccountID, frame.preKey),
		SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE,
	}
	for _, sensitive := range []bool{false, true} {
		input.IncludePlaintextText = sensitive
		result := engine.DecryptMessage(t.Context(), input)
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		decrypted := result.DecryptedMessage
		if decrypted.GetContent().GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT || result.MessageKind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE {
			t.Fatal("decrypted template was not projected as a text message")
		}
		if len(result.Candidates) != 1 || result.Candidates[0].GetKind() != waappv1.CandidateKind_CANDIDATE_KIND_OTP {
			t.Fatal("template verification code was not extracted")
		}
		wantText, wantOTP := "", ""
		if sensitive {
			wantText, wantOTP = "Your verification code is 012345.", "012345"
		}
		if decrypted.GetPlaintextText().GetValue() != wantText || result.Candidates[0].GetText().GetValue() != wantOTP {
			t.Fatal("template text or verification code lost its value or redaction")
		}
	}
}

func TestWrappedBusinessMessageQuote(t *testing.T) {
	template := &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
		HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{HydratedContentText: proto.String("Your code is 012345.")},
	}}
	wrapped := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: template}}
	text, content, _ := ProjectMessage(wrapped, true)
	if text != "Your code is 012345." || !content.GetEphemeral() {
		t.Fatal("wrapped template lost its text or ephemeral flag")
	}
	template.TemplateMessage.ContextInfo = &waE2E.ContextInfo{
		StanzaID: proto.String("original"), Participant: proto.String("12025550124@s.whatsapp.net"),
		QuotedMessage: &waE2E.Message{Conversation: proto.String("quoted secret")},
	}
	for _, sensitive := range []bool{false, true} {
		_, content, _ := ProjectMessage(wrapped, sensitive)
		quote := content.GetQuote()
		if quote.GetProviderMessageId() != "original" || (quote.GetText().GetValue() == "quoted secret") != sensitive {
			t.Fatal("template quote lost its target or redaction")
		}
		_, reply, _ := ProjectMessage(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("reply"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("template"), QuotedMessage: wrapped},
		}}, sensitive)
		if (reply.GetQuote().GetText().GetValue() == "Your code is 012345.") != sensitive {
			t.Fatal("quoted template lost its body or redaction")
		}
	}
}

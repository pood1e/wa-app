package communication

import (
	"context"
	"fmt"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"

	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const DefaultMessageReceiptTimeout = 15 * time.Second

func (e *messagingService) SendMessageReceipts(ctx context.Context, input wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult {
	if e == nil {
		return wacore.EngineMessageReceiptResult{Err: fmt.Errorf("native engine is required")}
	}
	messages := NormalizeMessageReceipts(input.Messages)
	if len(messages) == 0 {
		return wacore.EngineMessageReceiptResult{}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	timeout := input.RemoteTimeout
	if timeout <= 0 {
		timeout = DefaultMessageReceiptTimeout
	}
	client := NewChatdClient(ChatdConfigForState(proxyURL, state, timeout))
	session, err := client.OpenSession(ctx, state, input.RegisteredIdentityID, DefaultLoginPayload, input.AppVersion)
	if err != nil {
		return wacore.EngineMessageReceiptResult{Err: ChatdReceiveError(err)}
	}
	defer session.Close()
	if ApplyChatdSessionUpdateState(&state, session.Update()) {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	return session.SendMessageReceipts(ctx, messages, input.Type)
}

func (s *ChatdSession) SendMessageReceipts(ctx context.Context, messages []wacore.EngineMessageReceipt, receiptType waappv1.MessageReceiptType) wacore.EngineMessageReceiptResult {
	nodes := BuildMessageReceiptNodes(messages, receiptType)
	if len(nodes) == 0 && len(messages) > 0 {
		return wacore.EngineMessageReceiptResult{Err: fmt.Errorf("unsupported message receipt type")}
	}
	if err := ctx.Err(); err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	finish, err := s.bindDeadline(ctx, time.Now().Add(ContextBoundTimeout(ctx, DefaultMessageReceiptTimeout)))
	if err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	defer finish()
	sent := 0
	for _, node := range nodes {
		if err := s.Transport.SendNode(node); err != nil {
			return wacore.EngineMessageReceiptResult{Sent: sent, Err: ChatdReceiveError(err)}
		}
		sent += MessageReceiptNodeMessageCount(node)
	}
	return wacore.EngineMessageReceiptResult{Sent: sent}
}

func NormalizeMessageReceipts(messages []wacore.EngineMessageReceipt) []wacore.EngineMessageReceipt {
	out := make([]wacore.EngineMessageReceipt, 0, len(messages))
	seen := map[string]struct{}{}
	for _, message := range messages {
		message.ChatJID = strings.TrimSpace(message.ChatJID)
		message.ParticipantJID = strings.TrimSpace(message.ParticipantJID)
		message.ProviderMessageID = strings.TrimSpace(message.ProviderMessageID)
		if message.ChatJID == "" || message.ProviderMessageID == "" {
			continue
		}
		key := strings.Join([]string{message.ChatJID, message.ParticipantJID, message.ProviderMessageID}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, message)
	}
	return out
}

func BuildMessageReceiptNodes(messages []wacore.EngineMessageReceipt, receiptType waappv1.MessageReceiptType) []pwire.ChatdNode {
	if receiptType != waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED && receiptType != waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ {
		return nil
	}
	groups := map[string][]wacore.EngineMessageReceipt{}
	order := []string{}
	for _, message := range messages {
		key := strings.Join([]string{message.ChatJID, message.ParticipantJID}, "\x00")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], message)
	}
	nodes := make([]pwire.ChatdNode, 0, len(groups))
	for _, key := range order {
		items := groups[key]
		if len(items) == 0 {
			continue
		}
		attrs := map[string]string{
			"to": items[0].ChatJID,
			"id": items[0].ProviderMessageID,
			"t":  fmt.Sprint(time.Now().Unix()),
		}
		if receiptType == waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ {
			attrs["type"] = "read"
		}
		if participant := items[0].ParticipantJID; participant != "" && participant != items[0].ChatJID {
			attrs["participant"] = participant
		}
		node := pwire.ChatdNode{Tag: "receipt", Attrs: attrs}
		if len(items) > 1 {
			children := make([]pwire.ChatdNode, 0, len(items)-1)
			for _, item := range items[1:] {
				children = append(children, pwire.ChatdNode{Tag: "item", Attrs: map[string]string{"id": item.ProviderMessageID}})
			}
			node.Content = []pwire.ChatdNode{{Tag: "list", Content: children}}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func MessageReceiptNodeMessageCount(node pwire.ChatdNode) int {
	count := 1
	for _, child := range pwire.ChatdChildren(node) {
		if child.Tag != "list" {
			continue
		}
		count += len(pwire.ChatdChildren(child))
	}
	return count
}

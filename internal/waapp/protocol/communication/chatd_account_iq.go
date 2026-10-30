package communication

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const (
	DefaultAccountIQTimeout                = 32 * time.Second
	DefaultAccountSettingsOpenTimeout      = 8 * time.Second
	DefaultAccountSettingsOperationTimeout = 45 * time.Second
	DefaultAccountSettingsProxyTTL         = DefaultAccountSettingsOperationTimeout + 5*time.Second
	AccountSettingsRequestTimeoutMessage   = "WA account settings request timed out"
	AccountSettingsIQTimeoutMessage        = "account settings iq timed out"
)

func (c *ChatdClient) SendAccountIQ(ctx context.Context, state pstate.NativeState, input wacore.EngineAccountSettingsInput, appVersion string, request pwire.ChatdNode) (pwire.ChatdNode, ChatdSessionUpdate, error) {
	return c.SendIQ(ctx, state, input.RegisteredIdentityID, appVersion, request, AccountSettingsIQTimeoutMessage)
}

func (c *ChatdClient) SendIQ(ctx context.Context, state pstate.NativeState, registeredIdentityID string, appVersion string, request pwire.ChatdNode, timeoutMessage string) (pwire.ChatdNode, ChatdSessionUpdate, error) {
	session, err := c.OpenSession(ctx, state, registeredIdentityID, DefaultLoginPayload, appVersion)
	if err != nil {
		return pwire.ChatdNode{}, ChatdSessionUpdate{}, err
	}
	defer session.Close()
	response, _, update, err := session.SendIQ(ctx, wacore.EngineMessageInput{}, request, c.Cfg.Timeout, timeoutMessage)
	return response, update, err
}

func (s *ChatdSession) SendIQ(ctx context.Context, input wacore.EngineMessageInput, request pwire.ChatdNode, timeout time.Duration, timeoutMessage string) (pwire.ChatdNode, []ChatdReceivedItem, ChatdSessionUpdate, error) {
	if s == nil || s.Conn == nil {
		return pwire.ChatdNode{}, nil, ChatdSessionUpdate{}, fmt.Errorf("chatd session is not open")
	}
	if timeout <= 0 {
		timeout = DefaultChatdReadWindow
	}
	transport := &s.Transport
	update := s.Update()
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if !deadline.After(time.Now()) {
		return pwire.ChatdNode{}, nil, update, errors.New(timeoutMessage)
	}
	finish, err := s.bindDeadline(ctx, deadline)
	if err != nil {
		return pwire.ChatdNode{}, nil, update, err
	}
	defer finish()
	if err := transport.SendNode(request); err != nil {
		return pwire.ChatdNode{}, nil, update, pwire.ChatdPhase("chatd iq write", err)
	}
	items := []ChatdReceivedItem{}
	requestID := request.Attrs["id"]
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return pwire.ChatdNode{}, items, update, ctx.Err()
		}
		node, err := transport.ReadNode()
		if err != nil {
			if ctx.Err() != nil {
				return pwire.ChatdNode{}, items, update, ctx.Err()
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return pwire.ChatdNode{}, items, update, errors.New(timeoutMessage)
			}
			return pwire.ChatdNode{}, items, update, pwire.ChatdPhase("chatd iq read", err)
		}
		nextUpdate, nextItems, err := s.ConsumeIncomingNode(input, node, update, time.Now())
		update = nextUpdate
		items = append(items, nextItems...)
		if err != nil {
			return pwire.ChatdNode{}, items, update, err
		}
		if node.Tag == "iq" && node.Attrs["id"] == requestID {
			return node, items, update, nil
		}
	}
	return pwire.ChatdNode{}, items, update, errors.New(timeoutMessage)
}

func ChatdIQError(node pwire.ChatdNode) error {
	if node.Attrs["type"] != "error" {
		return nil
	}
	message := "WA request was rejected"
	if errorNode, ok := ChatdChild(node, "error"); ok {
		if code := strings.TrimSpace(errorNode.Attrs["code"]); code != "" {
			message = message + ChatdIQErrorCodeMessage(code)
		}
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, false)
}

func ChatdIQErrorCodeMessage(code string) string {
	if strings.HasPrefix(code, "<tok:") && strings.HasSuffix(code, ">") {
		return " (upstream error code could not be decoded)"
	}
	if code == "533" {
		return " (WA rejected this operation for this account right now; code 533)"
	}
	return " (code " + code + ")"
}

func ChatdChild(node pwire.ChatdNode, tag string) (pwire.ChatdNode, bool) {
	for _, child := range pwire.ChatdChildren(node) {
		if child.Tag == tag {
			return child, true
		}
	}
	return pwire.ChatdNode{}, false
}

func ChatdNodeValue(node pwire.ChatdNode, name string) string {
	if value := strings.TrimSpace(node.Attrs[name]); value != "" {
		return value
	}
	if child, ok := ChatdChild(node, name); ok {
		return ChatdNodeText(child)
	}
	return ""
}

func ChatdNodeText(node pwire.ChatdNode) string {
	switch value := node.Content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func ChatdNodeBool(node pwire.ChatdNode, name string) bool {
	value, _ := ChatdNodeBoolValue(node, name)
	return value
}

func ChatdNodeBoolValue(node pwire.ChatdNode, name string) (bool, bool) {
	value, ok := ChatdNodeStringValue(node, name)
	if !ok {
		return false, false
	}
	switch strings.ToLower(value) {
	case "true", "1", "yes", "ok", "success":
		return true, true
	case "false", "0", "no", "fail", "failed":
		return false, true
	default:
		return false, false
	}
}

func ChatdNodeStringValue(node pwire.ChatdNode, name string) (string, bool) {
	if value := strings.TrimSpace(node.Attrs[name]); value != "" {
		return value, true
	}
	child, ok := ChatdChild(node, name)
	if !ok {
		return "", false
	}
	if value := strings.TrimSpace(ChatdNodeText(child)); value != "" {
		return value, true
	}
	for _, attr := range []string{"status", "result", "code"} {
		if value := strings.TrimSpace(child.Attrs[attr]); value != "" {
			return value, true
		}
	}
	return "", true
}

func ChatdNodeStatus(node pwire.ChatdNode, name string) string {
	value, _ := ChatdNodeStringValue(node, name)
	return strings.ToLower(strings.TrimSpace(value))
}

func ChatdNodeDuration(node pwire.ChatdNode, name string) time.Duration {
	value := strings.TrimSpace(ChatdNodeValue(node, name))
	if value == "" {
		return 0
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

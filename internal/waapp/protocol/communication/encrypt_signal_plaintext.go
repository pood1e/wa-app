package communication

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func EncryptSignalPlaintext(state *pstate.NativeState, session *pstate.NativeSignalSession, plaintext []byte) ([]byte, error) {
	if session.SenderChain == nil {
		return nil, fmt.Errorf("missing sender chain")
	}
	chainKey, err := pwire.DecodeB64Any(session.SenderChain.ChainKey)
	if err != nil {
		return nil, err
	}
	ratchetPublic, err := pwire.DecodeB64Any(shared.FirstNonEmpty(session.SenderChain.RatchetKey, session.SenderRatchetPublic))
	if err != nil {
		return nil, err
	}
	ratchetPublic, err = pwire.WithSignalCurvePrefix(ratchetPublic)
	if err != nil {
		return nil, err
	}
	version := SignalMessageVersion(session.Version)
	counter := session.SenderChain.Index
	keys := DeriveMessageKeys(chainKey)
	ciphertext, err := pwire.AesCBCPKCS7Encrypt(plaintext, keys.CipherKey, keys.Iv)
	if err != nil {
		return nil, err
	}
	body := []byte{byte(version<<4 | version)}
	body = ProtoBytesInto(body, 1, ratchetPublic)
	body = ProtoVarintInto(body, 2, uint64(counter))
	if session.PreviousCounter != nil {
		body = ProtoVarintInto(body, 3, uint64(*session.PreviousCounter))
	}
	body = ProtoBytesInto(body, 4, ciphertext)
	identityPublic, err := state.Signal.Identity.PublicBytes()
	if err != nil {
		return nil, err
	}
	remoteIdentity, err := pwire.DecodeB64Any(session.RemoteIdentityPublic)
	if err != nil {
		return nil, err
	}
	mac, err := SignalMessageMAC(keys.MacKey, identityPublic, remoteIdentity, body, version)
	if err != nil {
		return nil, err
	}
	session.SenderChain.ChainKey = pwire.B64u(NextChainKey(chainKey))
	session.SenderChain.Index = counter + 1
	return append(body, mac...), nil
}

func NativeTextMessagePlaintext(text string) []byte {
	return ProtoBytes(1, []byte(text))
}

func SignalMessageVersion(version int) int {
	if version == 3 || version == 4 {
		return version
	}
	return 3
}

func (s *ChatdSession) SendMessageWithAck(ctx context.Context, input wacore.EngineMessageInput, node pwire.ChatdNode, providerID string, timeout time.Duration) ([]ChatdReceivedItem, ChatdSessionUpdate, error) {
	if s == nil || s.Conn == nil {
		return nil, ChatdSessionUpdate{}, fmt.Errorf("chatd session is not open")
	}
	deadline := TextMessageSendDeadline(ctx, timeout)
	update := s.Update()
	if !deadline.After(time.Now()) {
		return nil, update, errors.New(TextMessageAckTimeoutMessage)
	}
	transport := &s.Transport
	finish, err := s.bindDeadline(ctx, deadline)
	if err != nil {
		return nil, update, err
	}
	defer finish()
	if err := transport.SendNode(node); err != nil {
		return nil, update, pwire.ChatdPhase("chatd message write", err)
	}
	items := []ChatdReceivedItem{}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return items, update, ctx.Err()
		}
		next, err := transport.ReadNode()
		if err != nil {
			if ctx.Err() != nil {
				return items, update, ctx.Err()
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return items, update, errors.New(TextMessageAckTimeoutMessage)
			}
			return items, update, pwire.ChatdPhase("chatd message read", err)
		}
		if err := ChatdMessageSendRejection(next, providerID); err != nil {
			return items, update, err
		}
		acknowledged := IsChatdMessageSendAck(next, providerID)
		nextUpdate, nextItems, err := s.ConsumeIncomingNode(input, next, update, time.Now())
		update = nextUpdate
		items = append(items, nextItems...)
		if err != nil {
			return items, update, err
		}
		if acknowledged {
			return items, update, nil
		}
	}
	return items, update, errors.New(TextMessageAckTimeoutMessage)
}

func TextMessageSendDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(TextMessageSendTimeout(timeout))
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	return deadline
}

func IsChatdMessageSendAck(node pwire.ChatdNode, providerID string) bool {
	if providerID == "" || node.Attrs["id"] != providerID {
		return false
	}
	switch node.Tag {
	case "ack":
		return node.Attrs["class"] == "" || node.Attrs["class"] == "message"
	case "receipt":
		return node.Attrs["type"] == "" || node.Attrs["type"] == "read" || node.Attrs["type"] == "played"
	default:
		return false
	}
}

func ChatdMessageSendRejection(node pwire.ChatdNode, providerID string) error {
	if providerID == "" || node.Attrs["id"] != providerID {
		return nil
	}
	if code := strings.TrimSpace(node.Attrs["error"]); code != "" && code != "0" {
		return fmt.Errorf("WA text message was rejected: code %s", pstate.SafeResponseSnippet(code))
	}
	if node.Attrs["type"] == "error" {
		return fmt.Errorf("WA text message was rejected")
	}
	if node.Tag == "failure" || node.Tag == "error" {
		return fmt.Errorf("WA text message was rejected")
	}
	if errorNode, ok := ChatdChild(node, "error"); ok {
		if code := strings.TrimSpace(errorNode.Attrs["code"]); code != "" {
			return fmt.Errorf("WA text message was rejected: code %s", pstate.SafeResponseSnippet(code))
		}
		return fmt.Errorf("WA text message was rejected")
	}
	return nil
}

func TextMessageSendTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return DefaultTextMessageSendTimeout
	}
	return timeout
}

func ChatdSendError(err error) error {
	if IsAuthenticationFailure(err) {
		return err
	}
	message := "native chatd send failed"
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		message += ": timeout"
	} else {
		message = ChatdFailureMessage(message, err)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, true)
}

func NewTextProviderMessageID(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if ValidTextProviderMessageID(clientID) {
		return clientID
	}
	return "3EB0" + strings.ToUpper(hex.EncodeToString(pwire.RandomBytes(8)))
}

func ValidTextProviderMessageID(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

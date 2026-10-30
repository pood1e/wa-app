package communication

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestIQFailureClosesUnusableTransport(t *testing.T) {
	for _, failure := range []string{"partial write", "EOF", "invalid ciphertext", "authentication rejection", "stream end"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
				defer connection.Close()
				session, server, peer := syntheticChatdPipe(t)
				connection.installSession(session)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				sent := make(chan error, 1)
				go func() {
					if failure == "partial write" {
						_, err := io.ReadFull(peer, make([]byte, 4))
						sent <- err
						return
					}
					if _, err := server.ReadNode(); err != nil {
						sent <- err
						return
					}
					switch failure {
					case "EOF":
						sent <- peer.Close()
					case "invalid ciphertext":
						sent <- wire.ChatdWriteFrame(server.Rw.Writer, make([]byte, 32))
					case "authentication rejection":
						sent <- server.SendNode(wire.ChatdNode{Tag: "failure", Attrs: map[string]string{"reason": "403"}})
					case "stream end":
						sent <- server.SendNode(wire.ChatdNode{Tag: "xmlstreamend"})
					}
				}()
				_, _, err := connection.SendIQ(ctx, state.NativeState{}, "", "", wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "fixture", "type": "get"}}, "fixture timeout")
				if err == nil || session.Transport.failure == nil || connection.Session != nil {
					t.Fatal("failed transport remained available for reuse", err)
				}
				if failure == "authentication rejection" && !IsAuthenticationFailure(err) {
					t.Fatal("definitive authentication rejection lost its terminal status", err)
				}
				if err := <-sent; err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestRejectedLoginCheckRetainsHealthySocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session, server := syntheticChatdPair(t)
		connection.installSession(session)
		sent := make(chan error, 1)
		go func() {
			request, err := server.ReadNode()
			if err == nil {
				err = server.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": request.Attrs["id"], "type": "error"}, Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "501"}}}})
			}
			sent <- err
		}()
		result := connection.CheckLoginState(t.Context(), wacore.EngineLoginCheckInput{})
		if result.Err == nil || result.Status != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE || connection.Session != session {
			t.Fatal("rejected IQ closed a healthy transport or invalidated the account", result.Err)
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
	})
}

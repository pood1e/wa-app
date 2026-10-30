package communication

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestCancelledHeartbeatRetainsHealthySocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session, server := syntheticChatdPair(t)
		connection.installSession(session)
		input := wacore.EngineMessageInput{ClientProfileID: "current", WaitTimeout: 25 * time.Second}
		if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err != nil {
			t.Fatal(result.Err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan wacore.EngineMessageBatchResult, 1)
		go func() { done <- connection.ReceiveMessageBatch(ctx, input) }()
		if _, err := server.ReadNode(); err != nil {
			t.Fatal(err)
		}
		cancel()
		if result := <-done; result.Err == nil {
			t.Fatal("cancelled heartbeat did not report the operation failure")
		}
		if connection.Session != session {
			t.Fatal("cancelled heartbeat closed a healthy shared connection", session.Transport.failure)
		}
	})
}

func TestReceiverSendsKeepaliveAndRetainsInterleavedEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session, server := syntheticChatdPair(t)
		connection.installSession(session)
		input := wacore.EngineMessageInput{ClientProfileID: "current", WaitTimeout: 25 * time.Second}
		if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err != nil {
			t.Fatal(result.Err)
		}
		sent := make(chan error, 1)
		go func() {
			request, err := server.ReadNode()
			if err != nil {
				sent <- err
				return
			}
			if request.Tag != "iq" || request.Attrs["xmlns"] != "w:p" || request.Attrs["type"] != "get" || request.Attrs["to"] != "s.whatsapp.net" {
				sent <- wire.NewChatdError("expected protocol heartbeat")
				return
			}
			err = server.SendNode(wire.ChatdNode{Tag: "presence", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "type": "available"}})
			if err == nil {
				err = server.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": request.Attrs["id"], "type": "result"}})
			}
			sent <- err
		}()
		result := connection.ReceiveMessageBatch(t.Context(), input)
		if result.Err != nil || len(result.Presences) != 1 || connection.Session != session {
			t.Fatal("heartbeat lost an interleaved event or replaced the owned socket", result.Err)
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
		if !session.lastHeartbeatSuccess.Equal(time.Now()) {
			t.Fatal("heartbeat response was not recorded")
		}
	})
}

func TestHeartbeatTimeoutToleratesTransientLossAndReconnectsDeadSocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session, server := syntheticChatdPair(t)
		connection.installSession(session)
		go func() {
			for {
				if _, err := server.ReadNode(); err != nil {
					return
				}
			}
		}()
		input := wacore.EngineMessageInput{ClientProfileID: "current", WaitTimeout: 25 * time.Second}
		if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err != nil {
			t.Fatal(result.Err)
		}
		if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err != nil || connection.Session != session {
			t.Fatal("one missed heartbeat closed a resumable socket", result.Err)
		}
		for range 4 {
			if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err != nil {
				t.Fatal("transient heartbeat failures triggered an early reconnect", result.Err)
			}
		}
		if result := connection.ReceiveMessageBatch(t.Context(), input); result.Err == nil || connection.Session != nil {
			t.Fatal("unresponsive socket survived the heartbeat failure budget")
		}
	})
}

package communication

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestReceiverBudgetStartsAfterQueuedWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session, _ := syntheticChatdPair(t)
		connection.installSession(session)
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		done := make(chan wacore.EngineMessageBatchResult, 1)
		go func() {
			done <- connection.ReceiveMessageBatch(t.Context(), wacore.EngineMessageInput{ClientProfileID: "current", WaitTimeout: 25 * time.Second})
		}()
		synctest.Wait()
		time.Sleep(30 * time.Second)
		connection.operations.Release(1)
		result := <-done
		if result.Err != nil {
			t.Fatal("queued work exhausted the receiver's idle window", result.Err)
		}
		if connection.Session != session {
			t.Fatal("queued receiver replaced its healthy socket")
		}
		if elapsed := time.Since(started); elapsed != 55*time.Second {
			t.Fatalf("queued receive took %v, want 30s queueing plus a 25s idle window", elapsed)
		}
	})
}

func TestInterruptedIQRetainsSocketAndResumesLateResponse(t *testing.T) {
	for _, interruption := range []string{"header", "payload", "timeout"} {
		t.Run(interruption, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
				defer connection.Close()
				session, server := syntheticChatdPair(t)
				connection.installSession(session)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				first := make(chan error, 1)
				go func() {
					_, _, err := connection.SendIQ(ctx, state.NativeState{}, "", "", wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "first", "type": "get"}}, "fixture timeout")
					first <- err
				}()
				if _, err := server.ReadNode(); err != nil {
					t.Fatal(err)
				}
				encoded, err := server.Codec.EncodeNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "first", "type": "result"}})
				if err != nil {
					t.Fatal(err)
				}
				ciphertext, err := server.Keys.Encrypt(append([]byte{0}, encoded...))
				if err != nil {
					t.Fatal(err)
				}
				header, err := wire.ChatdInt24(len(ciphertext))
				if err != nil {
					t.Fatal(err)
				}
				frame := append(header, ciphertext...)
				cut := 2
				if interruption == "payload" {
					cut = len(header) + 5
				}
				if _, err := server.Rw.Write(frame[:cut]); err != nil {
					t.Fatal(err)
				}
				if err := server.Rw.Flush(); err != nil {
					t.Fatal(err)
				}
				if interruption == "timeout" {
					time.Sleep(DefaultAccountIQTimeout)
				} else {
					cancel()
				}
				if err := <-first; err == nil || (interruption != "timeout" && !errors.Is(err, context.Canceled)) {
					t.Fatal("interrupted IQ did not return its operation error", err)
				}
				if connection.Session != session {
					t.Fatal("interrupted response closed a resumable socket")
				}
				second := make(chan error, 1)
				go func() {
					response, _, err := connection.SendIQ(t.Context(), state.NativeState{}, "", "", wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "second", "type": "get"}}, "fixture timeout")
					if err == nil && response.Attrs["id"] != "second" {
						err = errors.New("late response was delivered to the next request")
					}
					second <- err
				}()
				if _, err := server.ReadNode(); err != nil {
					t.Fatal(err)
				}
				if _, err := server.Rw.Write(frame[cut:]); err != nil {
					t.Fatal(err)
				}
				if err := server.Rw.Flush(); err != nil {
					t.Fatal(err)
				}
				if err := server.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "second", "type": "result"}}); err != nil {
					t.Fatal(err)
				}
				if err := <-second; err != nil {
					t.Fatal("resuming an interrupted response lost frame or cipher state", err)
				}
			})
		})
	}
}

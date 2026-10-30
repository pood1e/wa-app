package communication

import (
	"bytes"
	"encoding/json"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestPQPreKeyMessageInteroperability(t *testing.T) {
	for _, lastResort := range []bool{true, false} {
		name := "one_time"
		if lastResort {
			name = "last_resort"
		}
		t.Run(name, func(t *testing.T) {
			parameters := protocoltest.AndroidClient()
			current, err := state.NewNativeState(&waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, parameters.GetMetadata(), parameters.GetMemoryGib())
			if err != nil {
				t.Fatal(err)
			}
			identity, err := current.Signal.Identity.PrivateBytes()
			if err != nil {
				t.Fatal(err)
			}
			pq, err := wire.NewSignalPQPreKey(identity, 29)
			if err != nil {
				t.Fatal(err)
			}
			if lastResort {
				current.Signal.PQLastResortPreKey = pq
			} else {
				current.Signal.PQOneTimePreKeys = append(current.Signal.PQOneTimePreKeys, pq)
			}
			want := []byte("synthetic PQ inbox")
			message := pqMessageFixture(t, current, pq, want)
			before, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			corrupt := bytes.Clone(message)
			corrupt[len(corrupt)-1] ^= 1
			if _, err := DecryptNativePKMsg(&current, corrupt, "synthetic-peer", true); err == nil {
				t.Fatal("corrupt KEM ciphertext authenticated")
			}
			after, err := json.Marshal(current)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed PQ message mutated account state")
			}
			preview, err := DecryptNativePKMsg(&current, message, "synthetic-peer", false)
			if err != nil || !bytes.Equal(preview.Plaintext, want) || len(current.Signal.Sessions) != 0 {
				t.Fatal("PQ preview failed or committed a session")
			}
			result, err := DecryptNativePKMsg(&current, message, "synthetic-peer", true)
			if err != nil || !bytes.Equal(result.Plaintext, want) || result.Version != 4 {
				t.Fatalf("independent PQ peer message failed: %v", err)
			}
			if lastResort && current.Signal.PQLastResortPreKey == nil || !lastResort && len(current.Signal.PQOneTimePreKeys) != 0 {
				t.Fatal("PQ prekey lifecycle differs from its key class")
			}
			if _, err := DecryptNativePKMsg(&current, message, "synthetic-peer", true); err == nil {
				t.Fatal("authenticated prekey message replay was accepted")
			}
			session := current.Signal.Sessions[SignalSenderKey("synthetic-peer")]
			if err := EnsureOutboundSignalChain(&session); err != nil {
				t.Fatal(err)
			}
			reply, err := EncryptSignalPlaintext(&current, &session, []byte("synthetic reply"))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseSignalMessage(reply)
			if err != nil || parsed.Version != 4 || len(parsed.RatchetKey) != 33 || parsed.RatchetKey[0] != 5 {
				t.Fatal("PQ session reply does not use the native Signal ratchet key encoding")
			}
		})
	}
}

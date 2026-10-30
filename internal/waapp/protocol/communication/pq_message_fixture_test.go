package communication

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

// The sending peer uses standard-library ML-KEM, HKDF, HMAC and CBC.
func pqMessageFixture(t *testing.T, receiver state.NativeState, pq *protocolv1.SignalPQPreKey, plaintext []byte) []byte {
	t.Helper()
	return signalConversationFixture(t, receiver, pq, [][]byte{plaintext})[0].preKey
}

type signalFixture struct{ preKey, message []byte }

func signalConversationFixture(t *testing.T, receiver state.NativeState, pq *protocolv1.SignalPQPreKey, plaintexts [][]byte) []signalFixture {
	t.Helper()
	generate := func() *ecdh.PrivateKey {
		t.Helper()
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	agree := func(private *ecdh.PrivateKey, public []byte) []byte {
		t.Helper()
		peer, err := ecdh.X25519().NewPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := private.ECDH(peer)
		if err != nil {
			t.Fatal(err)
		}
		return shared
	}
	expand := func(secret, salt []byte, label string, size int) []byte {
		t.Helper()
		out, err := hkdf.Key(sha256.New, secret, salt, label, size)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	identity, base, ratchet := generate(), generate(), generate()
	remoteSigned, err := receiver.Signal.SignedPreKey.KeyPair.PublicBytes()
	if err != nil {
		t.Fatal(err)
	}
	remoteIdentity, err := receiver.Signal.Identity.PublicBytes()
	if err != nil {
		t.Fatal(err)
	}
	master := bytes.Repeat([]byte{0xff}, 32)
	master = append(master, agree(identity, remoteSigned)...)
	master = append(master, agree(base, remoteIdentity)...)
	master = append(master, agree(base, remoteSigned)...)
	oneTime := receiver.Signal.OneTimePreKeys[0]
	oneTimePublic, err := oneTime.KeyPair.PublicBytes()
	if err != nil {
		t.Fatal(err)
	}
	master = append(master, agree(base, oneTimePublic)...)
	version, label := byte(0x33), "WhisperText"
	var kyberCiphertext []byte
	if pq != nil {
		kem, err := mlkem.NewEncapsulationKey1024(pq.GetPublic())
		if err != nil {
			t.Fatal(err)
		}
		shared, ciphertext := kem.Encapsulate()
		master = append(master, shared...)
		kyberCiphertext = ciphertext
		version, label = 0x44, "WhisperText_X25519_SHA-256_CRYSTALS-KYBER-1024"
	}
	initial := expand(master, nil, label, 64)
	chains := expand(agree(ratchet, remoteSigned), initial[:32], "WhisperRatchet", 64)
	chainKey := chains[32:]
	fixtures := make([]signalFixture, 0, len(plaintexts))
	for counter, plaintext := range plaintexts {
		seed := hmac.New(sha256.New, chainKey)
		_, _ = seed.Write([]byte{1})
		keys := expand(seed.Sum(nil), nil, "WhisperMessageKeys", 80)
		block, err := aes.NewCipher(keys[:32])
		if err != nil {
			t.Fatal(err)
		}
		padding := aes.BlockSize - len(plaintext)%aes.BlockSize
		padded := append(bytes.Clone(plaintext), bytes.Repeat([]byte{byte(padding)}, padding)...)
		encrypted := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, keys[64:]).CryptBlocks(encrypted, padded)
		message := fixtureBytes(nil, 1, append([]byte{5}, ratchet.PublicKey().Bytes()...))
		message = fixtureNumber(message, 2, uint64(counter))
		message = fixtureNumber(message, 3, 0)
		message = fixtureBytes(message, 4, encrypted)
		message = append([]byte{version}, message...)
		mac := hmac.New(sha256.New, keys[32:64])
		_, _ = mac.Write(append([]byte{5}, identity.PublicKey().Bytes()...))
		_, _ = mac.Write(append([]byte{5}, remoteIdentity...))
		_, _ = mac.Write(message)
		message = append(message, mac.Sum(nil)[:8]...)
		outer := fixtureNumber(nil, 1, uint64(oneTime.ID))
		outer = fixtureBytes(outer, 2, append([]byte{5}, base.PublicKey().Bytes()...))
		outer = fixtureBytes(outer, 3, append([]byte{5}, identity.PublicKey().Bytes()...))
		outer = fixtureBytes(outer, 4, message)
		outer = fixtureNumber(outer, 5, 1234)
		outer = fixtureNumber(outer, 6, uint64(receiver.Signal.SignedPreKey.ID))
		if pq != nil {
			outer = fixtureNumber(outer, 7, uint64(pq.GetId()))
			outer = fixtureBytes(outer, 8, kyberCiphertext)
		}
		fixtures = append(fixtures, signalFixture{preKey: append([]byte{version}, outer...), message: message})
		next := hmac.New(sha256.New, chainKey)
		_, _ = next.Write([]byte{2})
		chainKey = next.Sum(nil)
	}
	return fixtures
}

func fixtureBytes(out []byte, field protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(out, field, protowire.BytesType), value)
}

func fixtureNumber(out []byte, field protowire.Number, value uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(out, field, protowire.VarintType), value)
}

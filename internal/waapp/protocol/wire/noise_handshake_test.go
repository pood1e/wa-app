package wire_test

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/cloudflare/circl/kem/mlkem/mlkem512"
	stepx25519 "go.step.sm/crypto/x25519"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestNoiseInteroperability(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     protocolv1.HandshakePQMode
		protocol string
		pq       bool
		cache    bool
		resume   bool
		fs       bool
		second   bool
	}{
		{"XX", 0, "Noise_XX_25519_AESGCM_SHA256", false, false, false, false, false},
		{"XXkem", 1, "Noise_XXkem_25519_MLKEM512_AESGCM_SHA256", true, false, false, false, false},
		{"XXkem-FS", 2, "Noise_XXkem-FS_25519_MLKEM512_AESGCM_SHA256", true, false, false, false, false},
		{"XXkemEph", 9, "Noise_XXkemEph_25519_MLKEM512_AESGCM_SHA256", true, false, false, false, false},
		{"IK", 3, "Noise_IK_25519_AESGCM_SHA256", false, true, true, false, false},
		{"IKkem", 5, "Noise_IKkem+X25519+MLKEM512+AESGCM256+SHA256", true, true, true, false, false},
		{"IKkem-FS", 6, "Noise_IKkem-FS+X25519+MLKEM512+AESGCM256+SHA256", true, true, true, true, false},
		{"IKkem2", 8, "Noise_IKkem2+X25519+MLKEM512+AESGCM256+SHA256", true, true, true, true, true},
		{"missing_PQ_forces_full", 5, "Noise_XXkem_25519_MLKEM512_AESGCM_SHA256", true, false, false, false, false},
		{"IK_fallback", 3, "Noise_XXfallback_25519_AESGCM_SHA256", false, true, false, false, false},
		{"IKkem_fallback", 5, "Noise_XXkemfallback_25519_MLKEM512_AESGCM_SHA256", true, true, false, false, false},
		{"IKkem-FS_fallback", 6, "Noise_XXkem-FSfallback_25519_MLKEM512_AESGCM_SHA256", true, true, false, false, false},
		{"IKkem2_fallback", 8, "Noise_XXkemEphfallback_25519_MLKEM512_AESGCM_SHA256", true, true, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			peer, client, recovery := newHandshakeTest(t, test.mode, test.pq)
			peer.name, peer.resume, peer.pqResume, peer.fs, peer.second = test.protocol, test.resume, test.pq && test.resume, test.fs, test.second
			if !test.cache {
				recovery.ServerStaticPqPublic = nil
				if test.mode != 5 {
					recovery.ServerStaticPublic = ""
				}
			}
			keys, result, err := exchangeHandshake(t, peer, client, recovery)
			if err != nil {
				t.Fatalf("client handshake: %v", err)
			}
			if result.err != nil {
				t.Fatalf("independent peer handshake: %v", result.err)
			}
			if !bytes.Equal(keys.SendKey, result.receive) || !bytes.Equal(keys.RecvKey, result.send) {
				t.Fatal("client and independent peer derived different transport keys")
			}
			if !bytes.Equal(keys.ServerStaticPublic, peer.static.PublicKey().Bytes()) {
				t.Fatal("authenticated server key was not returned")
			}
			if test.pq {
				public, err := peer.pq.Public().MarshalBinary()
				if err != nil || !bytes.Equal(keys.ServerStaticPQPublic, public) {
					t.Fatal("server PQ recovery key was not returned")
				}
			}
		})
	}
}

func TestNoiseRejectsInvalidCertificateAndPQExtension(t *testing.T) {
	for _, kind := range []string{"signature", "trust_anchor", "PQ_header"} {
		t.Run(kind, func(t *testing.T) {
			peer, client, recovery := newHandshakeTest(t, 1, true)
			peer.name = "Noise_XXkem_25519_MLKEM512_AESGCM_SHA256"
			switch kind {
			case "signature":
				peer.corrupt = true
			case "trust_anchor":
				client.NoiseRootPublicKey = make([]byte, 32)
			case "PQ_header":
				peer.corruptTLV = true
			}
			keys, _, err := exchangeHandshake(t, peer, client, recovery)
			if err == nil || keys != nil {
				t.Fatal("unauthenticated or malformed handshake produced transport keys")
			}
		})
	}
}

func newHandshakeTest(t *testing.T, mode protocolv1.HandshakePQMode, pq bool) (*handshakePeer, *protocolv1.ClientMetadata, *protocolv1.ChatConnectionState) {
	t.Helper()
	rootPublic, rootPrivate, err := stepx25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer := &handshakePeer{root: rootPrivate, static: server, login: []byte("synthetic login")}
	recovery := &protocolv1.ChatConnectionState{ServerStaticPublic: wire.B64u(server.PublicKey().Bytes())}
	if pq {
		public, private, err := mlkem512.GenerateKeyPair(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		peer.pq = private
		recovery.ServerStaticPqPublic, err = public.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
	}
	return peer, &protocolv1.ClientMetadata{NoiseRootPublicKey: rootPublic, HandshakePqMode: mode}, recovery
}

func exchangeHandshake(t *testing.T, peer *handshakePeer, client *protocolv1.ClientMetadata, recovery *protocolv1.ChatConnectionState) (*wire.ChatdTransportKeys, peerResult, error) {
	t.Helper()
	identity, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer.client = identity.PublicKey().Bytes()
	left, right := net.Pipe()
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	if err := left.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := right.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	result := make(chan peerResult, 1)
	go func() { result <- peer.run(right) }()
	rw := bufio.NewReadWriter(bufio.NewReader(left), bufio.NewWriter(left))
	keys, err := wire.DoNoiseHandshake(rw, &protocolv1.CurveKeyPair{Public: peer.client, Private: identity.Bytes()}, client, recovery, peer.login, nil, 1<<20)
	_ = left.Close()
	return keys, <-result, err
}

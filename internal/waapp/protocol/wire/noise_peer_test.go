package wire_test

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	stepx25519 "go.step.sm/crypto/x25519"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// The peer uses standard-library HKDF/AEAD and a separate transcript implementation.
type peerNoise struct {
	hash, chain, key []byte
	counter          uint64
}

func newPeerNoise(name string) *peerNoise {
	initial := make([]byte, 32)
	if len(name) > 32 {
		digest := sha256.Sum256([]byte(name))
		copy(initial, digest[:])
	} else {
		copy(initial, name)
	}
	n := &peerNoise{hash: bytes.Clone(initial), chain: initial}
	n.hashData([]byte{'W', 'A', 6, 3})
	return n
}

func (n *peerNoise) hashData(data []byte) {
	digest := sha256.Sum256(append(bytes.Clone(n.hash), data...))
	n.hash = digest[:]
}

func (n *peerNoise) mix(data []byte) error {
	material, err := hkdf.Key(sha256.New, data, n.chain, "", 64)
	if err != nil {
		return err
	}
	n.chain, n.key, n.counter = material[:32], material[32:], 0
	return nil
}

func (n *peerNoise) dh(private *ecdh.PrivateKey, public []byte) error {
	key, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return err
	}
	shared, err := private.ECDH(key)
	if err != nil {
		return err
	}
	return n.mix(shared)
}

func (n *peerNoise) crypt(data []byte, decrypt bool) ([]byte, error) {
	block, err := aes.NewCipher(n.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], n.counter)
	n.counter++
	if decrypt {
		plain, err := aead.Open(nil, nonce, data, n.hash)
		if err == nil {
			n.hashData(data)
		}
		return plain, err
	}
	sealed := aead.Seal(nil, nonce, data, n.hash)
	n.hashData(sealed)
	return sealed, nil
}

func peerRead(rw *bufio.ReadWriter) (*protocolv1.HandshakeMessage, error) {
	var header [3]byte
	if _, err := io.ReadFull(rw, header[:]); err != nil {
		return nil, err
	}
	size := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
	if size > 1<<20 {
		return nil, errors.New("peer frame limit")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(rw, data); err != nil {
		return nil, err
	}
	message := new(protocolv1.HandshakeMessage)
	return message, proto.Unmarshal(data, message)
}

func peerWrite(rw *bufio.ReadWriter, message *protocolv1.HandshakeMessage) error {
	data, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	size := len(data)
	if _, err := rw.Write([]byte{byte(size >> 16), byte(size >> 8), byte(size)}); err != nil {
		return err
	}
	if _, err := rw.Write(data); err != nil {
		return err
	}
	return rw.Flush()
}

func peerCertificate(root []byte, server []byte, corrupt bool) ([]byte, error) {
	public, private, err := stepx25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	intermediate, err := proto.Marshal(&protocolv1.NoiseCertificateDetails{Serial: proto.Uint32(7), IssuerSerial: proto.Uint32(0), Key: public})
	if err != nil {
		return nil, err
	}
	leaf, err := proto.Marshal(&protocolv1.NoiseCertificateDetails{Serial: proto.Uint32(9), IssuerSerial: proto.Uint32(7), Key: server})
	if err != nil {
		return nil, err
	}
	intermediateSignature, err := stepx25519.Sign(rand.Reader, root, intermediate)
	if err != nil {
		return nil, err
	}
	leafSignature, err := stepx25519.Sign(rand.Reader, private, leaf)
	if err != nil {
		return nil, err
	}
	if corrupt {
		leafSignature[0] ^= 1
	}
	return proto.Marshal(&protocolv1.NoiseCertificateChain{
		Intermediate: &protocolv1.NoiseCertificate{Details: intermediate, Signature: intermediateSignature},
		Leaf:         &protocolv1.NoiseCertificate{Details: leaf, Signature: leafSignature},
	})
}

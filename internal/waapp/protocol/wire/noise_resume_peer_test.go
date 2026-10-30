package wire_test

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"

	"github.com/cloudflare/circl/kem/mlkem/mlkem512"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func (p *handshakePeer) resumeHandshake(rw *bufio.ReadWriter, n *peerNoise, hello *protocolv1.ClientHello) error {
	n.hashData(p.static.PublicKey().Bytes())
	n.hashData(hello.GetEphemeral())
	if p.pqResume && !p.second {
		ciphertext := hello.GetExtendedCiphertext()
		if len(ciphertext) != mlkem512.CiphertextSize {
			return errors.New("missing initial peer KEM ciphertext")
		}
		shared := make([]byte, mlkem512.SharedKeySize)
		p.pq.DecapsulateTo(shared, ciphertext)
		n.hashData(ciphertext)
		if err := n.mix(shared); err != nil {
			return err
		}
	}
	if p.fs {
		e := hello.GetExtendedEphemeral()
		if len(e) != 802 || !bytes.Equal(e[:2], []byte{0x11, 0}) {
			return errors.New("invalid peer KEM ephemeral encoding")
		}
		n.hashData(e)
	}
	if err := n.dh(p.static, hello.GetEphemeral()); err != nil {
		return err
	}
	var login []byte
	if p.pqResume {
		plain, err := n.crypt(hello.GetPayload(), true)
		if err != nil || len(plain) < 32 || !bytes.Equal(plain[:32], p.client) {
			return errors.New("invalid PQ peer login")
		}
		login = plain[32:]
		if err := n.dh(p.static, p.client); err != nil {
			return err
		}
	} else {
		static, err := n.crypt(hello.GetStatic(), true)
		if err != nil || !bytes.Equal(static, p.client) {
			return errors.New("invalid classical peer static")
		}
		if err := n.dh(p.static, static); err != nil {
			return err
		}
		login, err = n.crypt(hello.GetPayload(), true)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(login, p.login) {
		return errors.New("peer resume login mismatch")
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	n.hashData(ephemeral.PublicKey().Bytes())
	response := &protocolv1.ServerHello{Ephemeral: ephemeral.PublicKey().Bytes()}
	if p.fs {
		key, err := mlkem512.Scheme().UnmarshalBinaryPublicKey(hello.GetExtendedEphemeral()[2:])
		if err != nil {
			return err
		}
		ciphertext, shared, err := mlkem512.Scheme().Encapsulate(key)
		if err != nil {
			return err
		}
		n.hashData(ciphertext)
		if err := n.mix(shared); err != nil {
			return err
		}
		response.ExtendedCiphertext = append([]byte{4, 3, 0}, ciphertext...)
	}
	if err := n.dh(ephemeral, hello.GetEphemeral()); err != nil {
		return err
	}
	if err := n.dh(ephemeral, p.client); err != nil {
		return err
	}
	response.Payload, err = n.crypt([]byte("synthetic resume"), false)
	if err != nil {
		return err
	}
	return peerWrite(rw, &protocolv1.HandshakeMessage{ServerHello: response})
}

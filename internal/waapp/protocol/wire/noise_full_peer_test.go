package wire_test

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net"

	"github.com/cloudflare/circl/kem/mlkem/mlkem512"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

type handshakePeer struct {
	name       string
	root       []byte
	static     *ecdh.PrivateKey
	pq         *mlkem512.PrivateKey
	resume     bool
	pqResume   bool
	fs         bool
	second     bool
	corrupt    bool
	corruptTLV bool
	login      []byte
	client     []byte
}

type peerResult struct {
	send, receive []byte
	err           error
}

func (p *handshakePeer) run(conn net.Conn) peerResult {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	var prologue [4]byte
	if _, err := io.ReadFull(rw, prologue[:]); err != nil || prologue != [4]byte{'W', 'A', 6, 3} {
		return peerResult{err: errors.New("invalid peer prologue")}
	}
	message, err := peerRead(rw)
	if err != nil {
		return peerResult{err: err}
	}
	hello := message.GetClientHello()
	n := newPeerNoise(p.name)
	if p.resume {
		err = p.resumeHandshake(rw, n, hello)
	} else {
		err = p.fullHandshake(rw, n, hello)
	}
	if err != nil {
		return peerResult{err: err}
	}
	keys, err := hkdf.Key(sha256.New, nil, n.chain, "", 64)
	if err != nil {
		return peerResult{err: err}
	}
	return peerResult{send: keys[32:], receive: keys[:32]}
}

func (p *handshakePeer) fullHandshake(rw *bufio.ReadWriter, n *peerNoise, hello *protocolv1.ClientHello) error {
	n.hashData(hello.GetEphemeral())
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	n.hashData(ephemeral.PublicKey().Bytes())
	if err := n.dh(ephemeral, hello.GetEphemeral()); err != nil {
		return err
	}
	serverStatic, err := n.crypt(p.static.PublicKey().Bytes(), false)
	if err != nil {
		return err
	}
	if err := n.dh(p.static, hello.GetEphemeral()); err != nil {
		return err
	}
	certificate, err := peerCertificate(p.root, p.static.PublicKey().Bytes(), p.corrupt)
	if err != nil {
		return err
	}
	if p.pq != nil {
		public, err := p.pq.Public().MarshalBinary()
		if err != nil {
			return err
		}
		prefix := append([]byte{1, 0, 8}, []byte("MLKEM512")...)
		prefix = append(prefix, 2, 3, 0x20)
		prefix = append(prefix, public...)
		certificate = append(prefix, certificate...)
		if p.corruptTLV {
			certificate[1] = 0xff
		}
	}
	payload, err := n.crypt(certificate, false)
	if err != nil {
		return err
	}
	if err := peerWrite(rw, &protocolv1.HandshakeMessage{ServerHello: &protocolv1.ServerHello{
		Ephemeral: ephemeral.PublicKey().Bytes(), Static: serverStatic, Payload: payload}}); err != nil {
		return err
	}
	response, err := peerRead(rw)
	if err != nil {
		return err
	}
	finish := response.GetClientFinish()
	if p.pq != nil {
		ciphertext := finish.GetExtendedCiphertext()
		if len(ciphertext) != mlkem512.CiphertextSize {
			return errors.New("missing peer PQ finish")
		}
		shared := make([]byte, mlkem512.SharedKeySize)
		p.pq.DecapsulateTo(shared, ciphertext)
		n.hashData(ciphertext)
		if err := n.mix(shared); err != nil {
			return err
		}
	}
	clientStatic, err := n.crypt(finish.GetStatic(), true)
	if err != nil || !bytes.Equal(clientStatic, p.client) {
		return errors.New("peer client static mismatch")
	}
	if err := n.dh(ephemeral, clientStatic); err != nil {
		return err
	}
	login, err := n.crypt(finish.GetPayload(), true)
	if err != nil || !bytes.Equal(login, p.login) {
		return errors.New("peer login mismatch")
	}
	return nil
}

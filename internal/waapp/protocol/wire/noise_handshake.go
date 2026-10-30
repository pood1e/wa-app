package wire

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"

	"github.com/cloudflare/circl/kem/mlkem/mlkem512"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

type noiseHandshake struct {
	rw        *bufio.ReadWriter
	noise     *NoiseXXState
	mode      protocolv1.HandshakePQMode
	static    *ecdh.PrivateKey
	ephemeral *ecdh.PrivateKey
	root      []byte
	maxFrame  int
	login     []byte
}

// DoNoiseHandshake authenticates wa6 and negotiates one explicitly selected mode.
func DoNoiseHandshake(rw *bufio.ReadWriter, static *protocolv1.CurveKeyPair, client *protocolv1.ClientMetadata,
	connection *protocolv1.ChatConnectionState, loginPayload, routingInfo []byte, maxFrameBytes int) (*ChatdTransportKeys, error) {
	if err := ValidateHandshakeMode(client.GetHandshakePqMode()); err != nil {
		return nil, err
	}
	if len(client.GetNoiseRootPublicKey()) != 32 {
		return nil, errors.New("Noise certificate trust anchor is required")
	}
	key, err := ecdh.X25519().NewPrivateKey(static.GetPrivate())
	if err != nil || !bytes.Equal(key.PublicKey().Bytes(), static.GetPublic()) {
		return nil, errors.New("invalid Noise client key pair")
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, ChatdPhase("generate Noise ephemeral key", err)
	}
	h := &noiseHandshake{rw: rw, mode: client.GetHandshakePqMode(), static: key, ephemeral: ephemeral,
		root: client.GetNoiseRootPublicKey(), maxFrame: maxFrameBytes, login: loginPayload}
	if err := writeNoisePrologue(rw, routingInfo); err != nil {
		return nil, ChatdPhase("write Noise prologue", err)
	}
	if resumeHandshake(h.mode) && connection.GetServerStaticPublic() != "" {
		server, err := DecodeB64Any(connection.GetServerStaticPublic())
		if err != nil || len(server) != 32 {
			return nil, errors.New("invalid cached Noise server key")
		}
		pq := connection.GetServerStaticPqPublic()
		if len(pq) != 0 && len(pq) != mlkem512.PublicKeySize {
			return nil, errors.New("invalid cached Noise PQ server key")
		}
		if !isPQHandshake(h.mode) || len(pq) != 0 {
			return h.resume(server, pq)
		}
	}
	h.mode = fullHandshakeMode(h.mode)
	h.noise = NewNoiseState([]byte(noiseHandshakeName(h.mode, false, false)))
	hello := &protocolv1.ClientHello{Ephemeral: ephemeral.PublicKey().Bytes()}
	h.noise.MixHash(hello.GetEphemeral())
	if isPQHandshake(h.mode) {
		hello.PqMode = h.mode.Enum()
	}
	if err := writeHandshake(rw, &protocolv1.HandshakeMessage{ClientHello: hello}); err != nil {
		return nil, err
	}
	response, err := h.readServerHello()
	if err != nil {
		return nil, err
	}
	return h.finish(response)
}

func (h *noiseHandshake) finish(hello *protocolv1.ServerHello) (*ChatdTransportKeys, error) {
	h.noise.MixHash(hello.GetEphemeral())
	if err := h.mixDH(h.ephemeral, hello.GetEphemeral()); err != nil {
		return nil, err
	}
	server, err := h.noise.DecryptAndHash(hello.GetStatic())
	if err != nil || len(server) != 32 {
		return nil, errors.New("invalid Noise server static ciphertext")
	}
	if err := h.mixDH(h.ephemeral, server); err != nil {
		return nil, err
	}
	certificate, err := h.noise.DecryptAndHash(hello.GetPayload())
	if err != nil {
		return nil, ChatdPhase("decrypt Noise server payload", err)
	}
	var pq, ciphertext []byte
	if isPQHandshake(h.mode) {
		pq, certificate, err = parseNoisePQCertificate(certificate)
		if err != nil {
			return nil, err
		}
	}
	if err := verifyNoiseCertificate(certificate, server, h.root); err != nil {
		return nil, err
	}
	if pq != nil {
		ciphertext, err = h.encapsulate(pq)
		if err != nil {
			return nil, err
		}
	}
	encryptedStatic, err := h.noise.EncryptAndHash(h.static.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	if err := h.mixDH(h.static, hello.GetEphemeral()); err != nil {
		return nil, err
	}
	payload, err := h.noise.EncryptAndHash(h.login)
	if err != nil {
		return nil, err
	}
	finish := &protocolv1.ClientFinish{Static: encryptedStatic, Payload: payload, ExtendedCiphertext: ciphertext}
	if err := writeHandshake(h.rw, &protocolv1.HandshakeMessage{ClientFinish: finish}); err != nil {
		return nil, err
	}
	return h.transport(server, pq), nil
}

func (h *noiseHandshake) mixDH(private *ecdh.PrivateKey, public []byte) error {
	key, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return errors.New("invalid Noise peer public key")
	}
	shared, err := private.ECDH(key)
	if err != nil {
		return errors.New("invalid Noise key agreement")
	}
	h.noise.MixKey(shared)
	return nil
}

func (h *noiseHandshake) encapsulate(public []byte) ([]byte, error) {
	scheme := mlkem512.Scheme()
	key, err := scheme.UnmarshalBinaryPublicKey(public)
	if err != nil {
		return nil, errors.New("invalid Noise KEM public key")
	}
	ciphertext, shared, err := scheme.Encapsulate(key)
	if err != nil {
		return nil, errors.New("encapsulate Noise server key")
	}
	h.noise.MixHash(ciphertext)
	h.noise.MixKey(shared)
	return ciphertext, nil
}

func (h *noiseHandshake) transport(server, pq []byte) *ChatdTransportKeys {
	send, receive := h.noise.Split()
	return &ChatdTransportKeys{SendKey: send, RecvKey: receive, ServerStaticPublic: bytes.Clone(server), ServerStaticPQPublic: bytes.Clone(pq)}
}

func (h *noiseHandshake) readServerHello() (*protocolv1.ServerHello, error) {
	data, err := ChatdReadFrame(h.rw.Reader, h.maxFrame)
	if err != nil {
		return nil, ChatdPhase("read Noise server hello", err)
	}
	wrapper := new(protocolv1.HandshakeMessage)
	if err := proto.Unmarshal(data, wrapper); err != nil || wrapper.ServerHello == nil || wrapper.ClientHello != nil || wrapper.ClientFinish != nil {
		return nil, errors.New("invalid Noise server hello")
	}
	if len(wrapper.ServerHello.GetEphemeral()) != 32 || len(wrapper.ServerHello.GetPayload()) < 16 {
		return nil, errors.New("incomplete Noise server hello")
	}
	return wrapper.ServerHello, nil
}

func writeHandshake(rw *bufio.ReadWriter, message *protocolv1.HandshakeMessage) error {
	data, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	return ChatdWriteFrame(rw.Writer, data)
}

func writeNoisePrologue(rw *bufio.ReadWriter, routing []byte) error {
	if len(routing) > 0 {
		header, err := ChatdInt24(len(routing))
		if err != nil {
			return err
		}
		if _, err := rw.Write(append(append([]byte(WaRoutingPrefix), header...), routing...)); err != nil {
			return err
		}
	}
	if _, err := rw.WriteString(WaNoisePrologue); err != nil {
		return err
	}
	return rw.Flush()
}

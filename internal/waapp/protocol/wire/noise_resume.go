package wire

import (
	"bytes"
	"crypto/rand"
	"errors"

	"github.com/cloudflare/circl/kem/mlkem/mlkem512"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func (h *noiseHandshake) resume(server, pq []byte) (*ChatdTransportKeys, error) {
	h.noise = NewNoiseState([]byte(noiseHandshakeName(h.mode, true, false)))
	h.noise.MixHash(server)
	hello := &protocolv1.ClientHello{Ephemeral: h.ephemeral.PublicKey().Bytes()}
	h.noise.MixHash(hello.GetEphemeral())
	var ephemeralPQ *mlkem512.PrivateKey
	if isPQHandshake(h.mode) {
		hello.PqMode = h.mode.Enum()
		if h.mode != protocolv1.HandshakePQMode_IKKEM_2 {
			ciphertext, err := h.encapsulate(pq)
			if err != nil {
				return nil, err
			}
			hello.ExtendedCiphertext = ciphertext
		}
		if h.mode == protocolv1.HandshakePQMode_IKKEM_FS || h.mode == protocolv1.HandshakePQMode_IKKEM_2 {
			public, private, err := mlkem512.GenerateKeyPair(rand.Reader)
			if err != nil {
				return nil, errors.New("generate Noise KEM ephemeral key")
			}
			ephemeralPQ = private
			encoded, err := public.MarshalBinary()
			if err != nil {
				return nil, err
			}
			hello.ExtendedEphemeral = append([]byte{0x11, 0}, encoded...)
			h.noise.MixHash(hello.GetExtendedEphemeral())
		}
	}
	if err := h.mixDH(h.ephemeral, server); err != nil {
		return nil, err
	}
	if isPQHandshake(h.mode) {
		payload := append(bytes.Clone(h.static.PublicKey().Bytes()), h.login...)
		var err error
		hello.Payload, err = h.noise.EncryptAndHash(payload)
		if err != nil {
			return nil, err
		}
		if err := h.mixDH(h.static, server); err != nil {
			return nil, err
		}
	} else {
		var err error
		hello.Static, err = h.noise.EncryptAndHash(h.static.PublicKey().Bytes())
		if err != nil {
			return nil, err
		}
		if err := h.mixDH(h.static, server); err != nil {
			return nil, err
		}
		hello.Payload, err = h.noise.EncryptAndHash(h.login)
		if err != nil {
			return nil, err
		}
	}
	if err := writeHandshake(h.rw, &protocolv1.HandshakeMessage{ClientHello: hello}); err != nil {
		return nil, err
	}
	response, err := h.readServerHello()
	if err != nil {
		return nil, err
	}
	if len(response.GetStatic()) > 0 {
		h.noise = NewNoiseState([]byte(noiseHandshakeName(h.mode, false, true)))
		h.noise.MixHash(h.ephemeral.PublicKey().Bytes())
		return h.finish(response)
	}
	h.noise.MixHash(response.GetEphemeral())
	if ephemeralPQ != nil {
		ciphertext, rest, err := consumeNoiseTLV(response.GetExtendedCiphertext(), 4, mlkem512.CiphertextSize)
		if err != nil || len(rest) != 0 {
			return nil, errors.New("invalid Noise forward secrecy ciphertext")
		}
		shared := make([]byte, mlkem512.SharedKeySize)
		ephemeralPQ.DecapsulateTo(shared, ciphertext)
		h.noise.MixHash(ciphertext)
		h.noise.MixKey(shared)
	}
	if err := h.mixDH(h.ephemeral, response.GetEphemeral()); err != nil {
		return nil, err
	}
	if err := h.mixDH(h.static, response.GetEphemeral()); err != nil {
		return nil, err
	}
	if _, err := h.noise.DecryptAndHash(response.GetPayload()); err != nil {
		return nil, ChatdPhase("decrypt Noise resume payload", err)
	}
	return h.transport(server, pq), nil
}

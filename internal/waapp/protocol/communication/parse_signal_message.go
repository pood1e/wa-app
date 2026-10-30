package communication

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func ParseSignalMessage(raw []byte) (SignalMessage, error) {
	if len(raw) < 10 {
		return SignalMessage{}, fmt.Errorf("SignalMessage too short")
	}
	version := int(raw[0] >> 4)
	if version != 3 && version != 4 {
		return SignalMessage{}, fmt.Errorf("unsupported SignalMessage version %d", version)
	}
	fields, err := pwire.ParsePBFields(raw[1 : len(raw)-8])
	if err != nil {
		return SignalMessage{}, err
	}
	ratchetKey, err := pwire.FirstPBBytes(fields, 1, "ratchetKey")
	if err != nil {
		return SignalMessage{}, err
	}
	counter, err := pwire.FirstPBVarint(fields, 2, "counter", nil)
	if err != nil {
		return SignalMessage{}, err
	}
	defaultPrevious := uint64(^uint64(0))
	previous, err := pwire.FirstPBVarint(fields, 3, "previousCounter", &defaultPrevious)
	if err != nil {
		return SignalMessage{}, err
	}
	ciphertext, err := pwire.FirstPBBytes(fields, 4, "ciphertext")
	if err != nil {
		return SignalMessage{}, err
	}
	var previousCounter *int
	if previous != defaultPrevious {
		value := int(previous)
		previousCounter = &value
	}
	return SignalMessage{Version: version, RatchetKey: ratchetKey, Counter: int(counter), PreviousCounter: previousCounter, Ciphertext: ciphertext, Body: append([]byte{}, raw[:len(raw)-8]...), Mac: append([]byte{}, raw[len(raw)-8:]...), Raw: append([]byte{}, raw...)}, nil
}

func ParsePreKeySignalMessage(raw []byte) (PreKeySignalMessage, error) {
	if len(raw) < 2 {
		return PreKeySignalMessage{}, fmt.Errorf("PreKeySignalMessage too short")
	}
	version := int(raw[0] >> 4)
	if version != 3 && version != 4 {
		return PreKeySignalMessage{}, fmt.Errorf("unsupported PreKeySignalMessage version %d", version)
	}
	fields, err := pwire.ParsePBFields(raw[1:])
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	baseKey, err := pwire.FirstPBBytes(fields, 2, "baseKey")
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	identityKey, err := pwire.FirstPBBytes(fields, 3, "identityKey")
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	embedded, err := pwire.FirstPBBytes(fields, 4, "message")
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	signedPreKeyID, err := pwire.FirstPBVarint(fields, 6, "signedPreKeyId", nil)
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	minusOne := uint64(^uint64(0))
	preKeyID, _ := pwire.FirstPBVarint(fields, 1, "preKeyId", &minusOne)
	registrationID, _ := pwire.FirstPBVarint(fields, 5, "registrationId", &minusOne)
	kyberPreKeyID, _ := pwire.FirstPBVarint(fields, 7, "kyberPreKeyId", &minusOne)
	kyberCiphertext, _ := pwire.FirstPBBytes(fields, 8, "kyberCiphertext")
	if (kyberPreKeyID != minusOne) != (kyberCiphertext != nil) {
		return PreKeySignalMessage{}, fmt.Errorf("incomplete PQ prekey message")
	}
	if version == 4 && kyberCiphertext == nil {
		return PreKeySignalMessage{}, fmt.Errorf("version 4 requires PQ prekey material")
	}
	if kyberCiphertext != nil && (version != 4 || kyberPreKeyID < 1 || kyberPreKeyID > 0xffffff || len(kyberCiphertext) != 1568) {
		return PreKeySignalMessage{}, fmt.Errorf("invalid PQ prekey message")
	}
	message, err := ParseSignalMessage(embedded)
	if err != nil {
		return PreKeySignalMessage{}, err
	}
	out := PreKeySignalMessage{Version: version, SignedPreKeyID: int(signedPreKeyID), BaseKey: baseKey, IdentityKey: identityKey, Message: message}
	if preKeyID != minusOne {
		value := int(preKeyID)
		out.PreKeyID = &value
	}
	if registrationID != minusOne {
		value := int(registrationID)
		out.RegistrationID = &value
	}
	if kyberPreKeyID != minusOne {
		value := int(kyberPreKeyID)
		out.KyberPreKeyID = &value
	}
	out.KyberCiphertext = kyberCiphertext
	return out, nil
}

func DeriveX3DHInitial(masterSecret []byte) ([]byte, []byte) {
	expanded := pwire.SignalHKDFV3(masterSecret, []byte("WhisperText"), 64)
	return expanded[:32], expanded[32:64]
}

func RootRatchet(rootKey []byte, theirRatchetPublic []byte, ourRatchetPrivate []byte) ([]byte, []byte, error) {
	dh, err := pwire.NativeX25519Agree(ourRatchetPrivate, theirRatchetPublic)
	if err != nil {
		return nil, nil, err
	}
	expanded := pwire.HkdfExpand(pwire.HmacSHA256(rootKey, dh), []byte("WhisperRatchet"), 64)
	return expanded[:32], expanded[32:64], nil
}

func DeriveMessageKeys(chainKey []byte) *protocolv1.SignalMessageKeys {
	seed := pwire.HmacSHA256(chainKey, []byte{0x01})
	expanded := pwire.SignalHKDFV3(seed, []byte("WhisperMessageKeys"), 80)
	return &protocolv1.SignalMessageKeys{CipherKey: expanded[:32], MacKey: expanded[32:64], Iv: expanded[64:80]}
}

func NextChainKey(chainKey []byte) []byte {
	return pwire.HmacSHA256(chainKey, []byte{0x02})
}

func SignalMessageMAC(macKey []byte, senderIdentity []byte, receiverIdentity []byte, body []byte, version int) ([]byte, error) {
	mac := hmac.New(sha256.New, macKey)
	if version >= 3 {
		sender, err := pwire.WithSignalCurvePrefix(senderIdentity)
		if err != nil {
			return nil, err
		}
		receiver, err := pwire.WithSignalCurvePrefix(receiverIdentity)
		if err != nil {
			return nil, err
		}
		_, _ = mac.Write(sender)
		_, _ = mac.Write(receiver)
	}
	_, _ = mac.Write(body)
	return mac.Sum(nil)[:8], nil
}

func DecryptSignalWithChain(state *pstate.NativeState, msg SignalMessage, chain pstate.NativeReceiverChain, remoteIdentity []byte) ([]byte, pstate.NativeReceiverChain, error) {
	keys, next, err := receiverMessageKeys(chain, msg.Counter)
	if err != nil {
		return nil, pstate.NativeReceiverChain{}, err
	}
	if len(keys.GetCipherKey()) != 32 || len(keys.GetMacKey()) != 32 || len(keys.GetIv()) != 16 {
		return nil, pstate.NativeReceiverChain{}, fmt.Errorf("invalid saved Signal message keys")
	}
	identityPublic, err := state.Signal.Identity.PublicBytes()
	if err != nil {
		return nil, pstate.NativeReceiverChain{}, err
	}
	expected, err := SignalMessageMAC(keys.MacKey, remoteIdentity, identityPublic, msg.Body, msg.Version)
	if err != nil {
		return nil, pstate.NativeReceiverChain{}, err
	}
	if !hmac.Equal(expected, msg.Mac) {
		return nil, pstate.NativeReceiverChain{}, fmt.Errorf("bad SignalMessage MAC")
	}
	plaintext, err := pwire.AesCBCPKCS7Decrypt(msg.Ciphertext, keys.CipherKey, keys.Iv)
	if err != nil {
		return nil, pstate.NativeReceiverChain{}, err
	}
	return plaintext, next, nil
}

func CommitPKMsgSession(state *pstate.NativeState, sender string, pkmsg PreKeySignalMessage, signedPreKey pstate.NativeSignalPreKey, rootKey []byte, chain pstate.NativeReceiverChain) {
	state.EnsureMaps()
	senderKey := SignalSenderKey(sender)
	remoteIdentity, _ := pwire.WithSignalCurvePrefix(pkmsg.IdentityKey)
	ratchetKey, _ := pwire.WithSignalCurvePrefix(pkmsg.Message.RatchetKey)
	ratchetID := hex.EncodeToString(ratchetKey)
	signedPublic, _ := signedPreKey.KeyPair.PublicBytes()
	signedPrivate, _ := signedPreKey.KeyPair.PrivateBytes()
	session := pstate.NativeSignalSession{Sender: sender, Version: pkmsg.Message.Version, RemoteIdentityPublic: pwire.B64u(remoteIdentity), RootKey: pwire.B64u(rootKey), SenderRatchetPublic: pwire.B64u(signedPublic), SenderRatchetPrivate: pwire.B64u(signedPrivate), ReceiverChains: map[string]pstate.NativeReceiverChain{}}
	if pkmsg.Message.PreviousCounter != nil {
		value := *pkmsg.Message.PreviousCounter
		session.PreviousCounter = &value
	}
	if pkmsg.RegistrationID != nil {
		value := *pkmsg.RegistrationID
		session.RemoteRegistrationID = &value
	}
	session.AliceBaseKey = pwire.B64u(pkmsg.BaseKey)
	chain.RatchetKey, chain.RootKey = pwire.B64u(ratchetKey), pwire.B64u(rootKey)
	session.ReceiverChains[ratchetID] = chain
	state.Signal.RemoteIdentities[senderKey] = pwire.B64u(remoteIdentity)
	state.Signal.Sessions[senderKey] = session
}

func FindNativeOneTimePreKey(keys []pstate.NativeSignalPreKey, id int) (pstate.NativeSignalPreKey, bool) {
	for _, key := range keys {
		if int(key.ID) == id {
			return key, true
		}
	}
	return pstate.NativeSignalPreKey{}, false
}

func MatchingSignalSessions(sessions map[string]pstate.NativeSignalSession, sender string) map[string]pstate.NativeSignalSession {
	out := map[string]pstate.NativeSignalSession{}
	if session, ok := sessions[SignalSenderKey(sender)]; ok {
		out[SignalSenderKey(sender)] = session
	}
	if sender != "" {
		if session, ok := sessions[SignalSenderKey("")]; ok {
			out[SignalSenderKey("")] = session
		}
	}
	if len(out) == 0 {
		for key, session := range sessions {
			out[key] = session
		}
	}
	return out
}

func SignalSenderKey(sender string) string {
	if strings.TrimSpace(sender) == "" {
		return "__default__"
	}
	return strings.TrimSpace(sender)
}

func ErrorSuffix(errors []string) string {
	if len(errors) == 0 {
		return ""
	}
	if len(errors) > 12 {
		errors = append(errors[:12], fmt.Sprintf("... %d more", len(errors)-12))
	}
	return "; " + strings.Join(errors, "; ")
}

package communication

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type SignalMessage struct {
	Version         int
	RatchetKey      []byte
	Counter         int
	PreviousCounter *int
	Ciphertext      []byte
	Body            []byte
	Mac             []byte
	Raw             []byte
}

type PreKeySignalMessage struct {
	Version         int
	PreKeyID        *int
	SignedPreKeyID  int
	BaseKey         []byte
	IdentityKey     []byte
	RegistrationID  *int
	Message         SignalMessage
	KyberPreKeyID   *int
	KyberCiphertext []byte
}

type SignalDecryptOutput struct {
	EncType   string
	Variant   string
	Version   int
	Counter   int
	Plaintext []byte
}

func DecryptNativeSignalPayload(state *pstate.NativeState, payload pstate.NativeMessagePayload, commit bool) (SignalDecryptOutput, error) {
	state.EnsureMaps()
	enc, err := pwire.DecodeB64Any(payload.Payload)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	typeHint := NormalizeNativeSignalEncType(payload.EncType)
	if typeHint == "skmsg" {
		return decryptGroupMessage(state, payload.Contact, payload.Sender, enc, commit)
	}
	if typeHint == "auto" {
		var errors []string
		for _, candidate := range []string{"pkmsg", "msg"} {
			out, err := DecryptNativeSignalPayloadByType(state, enc, candidate, payload.Sender, commit)
			if err == nil {
				return out, nil
			}
			errors = append(errors, candidate+": "+err.Error())
		}
		return SignalDecryptOutput{}, fmt.Errorf("auto decrypt failed; %s", strings.Join(errors, "; "))
	}
	return DecryptNativeSignalPayloadByType(state, enc, typeHint, payload.Sender, commit)
}

func NormalizeNativeSignalEncType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto"
	case "<tok:50>":
		return "skmsg"
	case "<tok:77>":
		return "msg"
	case "<tok:83>":
		return "pkmsg"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func DecryptNativeSignalPayloadByType(state *pstate.NativeState, enc []byte, encType string, sender string, commit bool) (SignalDecryptOutput, error) {
	switch encType {
	case "pkmsg":
		return DecryptNativePKMsg(state, enc, sender, commit)
	case "msg":
		return DecryptNativeMsg(state, enc, sender, commit)
	default:
		return SignalDecryptOutput{}, fmt.Errorf("unsupported enc type %q; implemented: pkmsg,msg", encType)
	}
}

func DecryptNativePKMsg(state *pstate.NativeState, enc []byte, sender string, commit bool) (SignalDecryptOutput, error) {
	pkmsg, err := ParsePreKeySignalMessage(enc)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	if stored := state.Signal.RemoteIdentities[SignalSenderKey(sender)]; stored != "" {
		expected, err := pwire.DecodeB64Any(stored)
		if err != nil {
			return SignalDecryptOutput{}, fmt.Errorf("invalid saved peer identity")
		}
		expected, err = pwire.WithSignalCurvePrefix(expected)
		if err != nil {
			return SignalDecryptOutput{}, fmt.Errorf("invalid saved peer identity")
		}
		actual, err := pwire.WithSignalCurvePrefix(pkmsg.IdentityKey)
		if err != nil || !bytes.Equal(expected, actual) {
			return SignalDecryptOutput{}, fmt.Errorf("peer Signal identity changed")
		}
	}
	if session, ok := state.Signal.Sessions[SignalSenderKey(sender)]; ok && session.AliceBaseKey == pwire.B64u(pkmsg.BaseKey) {
		out, err := DecryptNativeMsg(state, pkmsg.Message.Raw, sender, commit)
		out.EncType = "pkmsg"
		return out, err
	}
	signedPreKey := state.Signal.SignedPreKey
	if int(signedPreKey.ID) != pkmsg.SignedPreKeyID {
		return SignalDecryptOutput{}, fmt.Errorf("signed prekey %d not found", pkmsg.SignedPreKeyID)
	}
	signedPrivate, err := signedPreKey.KeyPair.PrivateBytes()
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	identityPrivate, err := state.Signal.Identity.PrivateBytes()
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	master := bytes.Repeat([]byte{0xff}, 32)
	part, err := pwire.NativeX25519Agree(signedPrivate, pkmsg.IdentityKey)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	master = append(master, part...)
	part, err = pwire.NativeX25519Agree(identityPrivate, pkmsg.BaseKey)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	master = append(master, part...)
	part, err = pwire.NativeX25519Agree(signedPrivate, pkmsg.BaseKey)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	master = append(master, part...)
	if pkmsg.PreKeyID != nil {
		oneTime, ok := FindNativeOneTimePreKey(state.Signal.OneTimePreKeys, *pkmsg.PreKeyID)
		if !ok {
			return SignalDecryptOutput{}, fmt.Errorf("one-time prekey %d not found", *pkmsg.PreKeyID)
		}
		oneTimePrivate, err := oneTime.KeyPair.PrivateBytes()
		if err != nil {
			return SignalDecryptOutput{}, err
		}
		part, err = pwire.NativeX25519Agree(oneTimePrivate, pkmsg.BaseKey)
		if err != nil {
			return SignalDecryptOutput{}, err
		}
		master = append(master, part...)
	}
	var rootKey, initialSenderChain []byte
	if pkmsg.KyberPreKeyID != nil {
		shared, err := decapsulatePreKeyMessage(state, pkmsg)
		if err != nil {
			return SignalDecryptOutput{}, err
		}
		master = append(master, shared...)
		expanded := pwire.SignalHKDFV3(master, []byte("WhisperText_X25519_SHA-256_CRYSTALS-KYBER-1024"), 64)
		rootKey, initialSenderChain = expanded[:32], expanded[32:]
	} else {
		rootKey, initialSenderChain = DeriveX3DHInitial(master)
	}
	type attempt struct {
		variant  string
		chainKey []byte
		index    int
		rootKey  []byte
	}
	attempts := []attempt{}
	if newRoot, receiverChain, err := RootRatchet(rootKey, pkmsg.Message.RatchetKey, signedPrivate); err == nil {
		attempts = append(attempts, attempt{variant: "x3dh_root_ratchet_signed_prekey", chainKey: receiverChain, index: 0, rootKey: newRoot})
	}
	attempts = append(attempts, attempt{variant: "x3dh_initial_chain_fallback", chainKey: initialSenderChain, index: 0, rootKey: rootKey})
	var errors []string
	for _, item := range attempts {
		chain := pstate.NativeReceiverChain{ChainKey: pwire.B64u(item.chainKey), Index: item.index}
		plaintext, nextChain, err := DecryptSignalWithChain(state, pkmsg.Message, chain, pkmsg.IdentityKey)
		if err != nil {
			errors = append(errors, item.variant+": "+err.Error())
			continue
		}
		if commit {
			CommitPKMsgSession(state, sender, pkmsg, signedPreKey, item.rootKey, nextChain)
			consumePreKeys(state, pkmsg)
		}
		return SignalDecryptOutput{EncType: "pkmsg", Variant: item.variant, Version: pkmsg.Message.Version, Counter: pkmsg.Message.Counter, Plaintext: plaintext}, nil
	}
	return SignalDecryptOutput{}, fmt.Errorf("pkmsg decrypt failed; %s", strings.Join(errors, "; "))
}

func DecryptNativeMsg(state *pstate.NativeState, enc []byte, sender string, commit bool) (SignalDecryptOutput, error) {
	msg, err := ParseSignalMessage(enc)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	ratchetKey, err := pwire.WithSignalCurvePrefix(msg.RatchetKey)
	if err != nil {
		return SignalDecryptOutput{}, err
	}
	ratchetID := hex.EncodeToString(ratchetKey)
	var errors []string
	for key, session := range MatchingSignalSessions(state.Signal.Sessions, sender) {
		remoteIdentity, err := pwire.DecodeB64Any(session.RemoteIdentityPublic)
		if err != nil {
			errors = append(errors, key+": "+err.Error())
			continue
		}
		if chain, ok := session.ReceiverChains[ratchetID]; ok {
			plaintext, nextChain, err := DecryptSignalWithChain(state, msg, chain, remoteIdentity)
			if err != nil {
				errors = append(errors, key+": "+err.Error())
				continue
			}
			if commit {
				session.PendingPreKey = nil
				session.ReceiverChains[ratchetID] = nextChain
				state.Signal.Sessions[key] = session
			}
			return SignalDecryptOutput{EncType: "msg", Variant: "receiver_chain", Version: msg.Version, Counter: msg.Counter, Plaintext: plaintext}, nil
		}
		if session.RootKey == "" || session.SenderRatchetPrivate == "" {
			errors = append(errors, key+": no matching receiver chain and no root/sender ratchet private")
			continue
		}
		rootKey, err := pwire.DecodeB64Any(session.RootKey)
		if err != nil {
			errors = append(errors, key+": "+err.Error())
			continue
		}
		senderPrivate, err := pwire.DecodeB64Any(session.SenderRatchetPrivate)
		if err != nil {
			errors = append(errors, key+": "+err.Error())
			continue
		}
		newRoot, chainKey, err := RootRatchet(rootKey, msg.RatchetKey, senderPrivate)
		if err != nil {
			errors = append(errors, key+": "+err.Error())
			continue
		}
		chain := pstate.NativeReceiverChain{RatchetKey: pwire.B64u(ratchetKey), ChainKey: pwire.B64u(chainKey), RootKey: pwire.B64u(newRoot)}
		plaintext, nextChain, err := DecryptSignalWithChain(state, msg, chain, remoteIdentity)
		if err != nil {
			errors = append(errors, key+": "+err.Error())
			continue
		}
		if commit {
			session.PendingPreKey = nil
			if session.ReceiverChains == nil {
				session.ReceiverChains = map[string]pstate.NativeReceiverChain{}
			}
			session.RootKey = pwire.B64u(newRoot)
			session.ReceiverChains[ratchetID] = nextChain
			state.Signal.Sessions[key] = session
		}
		return SignalDecryptOutput{EncType: "msg", Variant: "root_ratchet", Version: msg.Version, Counter: msg.Counter, Plaintext: plaintext}, nil
	}
	return SignalDecryptOutput{}, fmt.Errorf("normal msg decrypt failed%s", ErrorSuffix(errors))
}

package communication

import (
	"encoding/binary"
	"errors"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/libsignal/keys/identity"
	"go.mau.fi/libsignal/keys/prekey"
	"go.mau.fi/libsignal/util/optional"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func preKeyBundleFromNode(node wire.ChatdNode) (*prekey.Bundle, error) {
	if _, ok := ChatdChild(node, "error"); ok {
		return nil, errors.New("peer prekey request was rejected")
	}
	registration, err := preKeyNodeBytes(node, "registration", 4)
	if err != nil {
		return nil, err
	}
	keys := node
	if nested, ok := ChatdChild(node, "keys"); ok {
		keys = nested
	}
	public, err := preKeyNodeBytes(keys, "identity", 32)
	if err != nil {
		return nil, err
	}
	signed, ok := ChatdChild(keys, "skey")
	if !ok {
		return nil, errors.New("peer signed prekey is missing")
	}
	signedID, signedPublic, err := parsePreKeyNode(signed)
	if err != nil {
		return nil, err
	}
	signature, err := preKeyNodeBytes(signed, "signature", 64)
	if err != nil {
		return nil, err
	}
	peerIdentity := ecc.NewDjbECPublicKey([32]byte(public))
	if !ecc.VerifySignature(peerIdentity, signedPublic.Serialize(), [64]byte(signature)) {
		return nil, errors.New("peer signed prekey signature is invalid")
	}
	oneTimeID := optional.NewEmptyUint32()
	var oneTime ecc.ECPublicKeyable
	if key, exists := ChatdChild(keys, "key"); exists {
		id, value, err := parsePreKeyNode(key)
		if err != nil {
			return nil, err
		}
		oneTimeID, oneTime = optional.NewOptionalUint32(id), value
	}
	return prekey.NewBundle(binary.BigEndian.Uint32(registration), 0, oneTimeID, signedID,
		oneTime, signedPublic, [64]byte(signature), identity.NewKey(peerIdentity)), nil
}

func parsePreKeyNode(node wire.ChatdNode) (uint32, ecc.ECPublicKeyable, error) {
	id, err := preKeyNodeBytes(node, "id", 3)
	if err != nil {
		return 0, nil, err
	}
	value, err := preKeyNodeBytes(node, "value", 32)
	if err != nil {
		return 0, nil, err
	}
	return uint32(id[0])<<16 | uint32(id[1])<<8 | uint32(id[2]), ecc.NewDjbECPublicKey([32]byte(value)), nil
}

func preKeyNodeBytes(node wire.ChatdNode, name string, size int) ([]byte, error) {
	child, exists := ChatdChild(node, name)
	value, ok := child.Content.([]byte)
	if !exists || !ok || len(value) != size {
		return nil, errors.New("peer prekey field has invalid size or type: " + name)
	}
	return value, nil
}

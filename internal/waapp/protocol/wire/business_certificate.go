package wire

import (
	"crypto/rand"
	"encoding/binary"

	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"google.golang.org/protobuf/proto"
)

// NewBusinessCertificate signs the native SMB name certificate with the account's Signal identity.
func NewBusinessCertificate(identityPrivate []byte, name string) ([]byte, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	serial := binary.BigEndian.Uint64(random[:]) & 0x00ffffffffffffff
	if serial == 0 {
		serial = 1
	}
	details, err := proto.Marshal(&waVnameCert.VerifiedNameCertificate_Details{
		Serial: proto.Uint64(serial), Issuer: proto.String("smb:wa"), VerifiedName: proto.String(name),
	})
	if err != nil {
		return nil, err
	}
	signature, err := XeddsaSignCurve25519(identityPrivate, details)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(&waVnameCert.VerifiedNameCertificate{Details: details, Signature: signature})
}

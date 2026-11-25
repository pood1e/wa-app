package wire

import (
	"bytes"
	"crypto/ecdh"
	"testing"
)

func TestSignalSignaturesPreserveTheNativeEdwardsSignBit(t *testing.T) {
	message := []byte("synthetic native ADV signature")
	for value := byte(0); value < 16; value++ {
		private := bytes.Repeat([]byte{value}, 32)
		key, err := ecdh.X25519().NewPrivateKey(private)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := SignalSignCurve25519(private, message)
		if err != nil {
			t.Fatal(err)
		}
		if signature[63]&0x80 == 0 {
			continue
		}
		public := key.PublicKey().Bytes()
		for _, encoded := range [][]byte{public, append([]byte{SignalCurvePrefix}, public...)} {
			if valid, err := SignalVerifyCurve25519(encoded, message, signature); err != nil || !valid {
				t.Fatal("valid native Signal signature was rejected", err)
			}
		}
		if valid, _ := XeddsaVerifyCurve25519(public, message, signature); valid {
			t.Fatal("native sign-bit fixture did not distinguish the signature variants")
		}
		signature[0] ^= 1
		if valid, err := SignalVerifyCurve25519(public, message, signature); err != nil || valid {
			t.Fatal("altered native signature was accepted", err)
		}
		return
	}
	t.Fatal("native Signal signatures omitted the Edwards sign bit")
}

package ios

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRegistrationAuthorizationPreservesNativeParts(t *testing.T) {
	identifier := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32))
	proof := []byte{0xff, 0xff, 0xff, 0xff}
	header, err := RegistrationAuthorization(identifier, proof)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(header, "|")
	if len(parts) != 2 || parts[0] != "_____w" || strings.Contains(header, "=") {
		t.Fatal("authorization lost the native separator or padding behavior")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || string(decoded) != identifier {
		t.Fatal("authorization must wrap the SDK Base64 key identifier as text")
	}
	if _, err := RegistrationAuthorization(identifier, nil); err == nil {
		t.Fatal("accepted missing key attestation")
	}
	if _, err := RegistrationAuthorization("invalid", proof); err == nil {
		t.Fatal("accepted invalid key identifier")
	}
}

func TestRegistrationBodyPreservesPaddingAndAssertion(t *testing.T) {
	frame := bytes.Repeat([]byte{0xff}, 49)
	assertion := []byte{0x43, 0xff, 0xff, 0xff}
	body, err := RegistrationBody(frame, assertion)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "ENC=") || !bytes.Contains(body, []byte("%3D%3D&H=")) {
		t.Fatal("registration form lost padded ENC or field order")
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	// Apple Foundation JSONEncoder fixture with synthetic CBOR bytes.
	const wantH = "eyJhc3NlcnRpb24iOiJRXC9cL1wvXC93PT0ifQ"
	if got := form.Get("H"); got != wantH {
		t.Fatalf("registration H = %q, want %q", got, wantH)
	}
	decoded, err := base64.URLEncoding.DecodeString(form.Get("ENC"))
	if err != nil || !bytes.Equal(decoded, frame) {
		t.Fatal("registration ENC changed the binary frame")
	}
	proof, err := base64.RawURLEncoding.DecodeString(form.Get("H"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Assertion []byte `json:"assertion"`
	}
	if err := json.Unmarshal(proof, &payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload.Assertion, assertion) {
		t.Fatal("registration H changed the App Attest assertion")
	}
}

func TestRegistrationBodyWithoutAssertion(t *testing.T) {
	body, err := RegistrationBody(make([]byte, 48), nil)
	if err != nil {
		t.Fatal(err)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(form) != 1 || form.Get("ENC") == "" {
		t.Fatal("registration without assertion must contain only ENC")
	}
	if _, err := RegistrationBody(make([]byte, 47), nil); err == nil {
		t.Fatal("accepted a truncated registration frame")
	}
}

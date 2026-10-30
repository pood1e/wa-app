// wa-appattest-check verifies remote signing using a synthetic ENC frame.
package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func main() {
	target := flag.String("target", "", "private Unix socket or loopback SSH forward")
	flag.Parse()
	if err := check(*target); err != nil {
		fmt.Fprintln(os.Stderr, "app attest check failed:", err)
		os.Exit(1)
	}
}

func check(target string) (err error) {
	connection, err := attestation.Dial(target)
	if err != nil {
		return err
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	attestor, err := ios.CreateAttestor(ctx, waappv1.NewAppAttestServiceClient(connection))
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		err = errors.Join(err, attestor.Close(cleanup))
	}()
	registrationKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	authorization, err := attestor.AttestKey(ctx, registrationKey.PublicKey().Bytes())
	if err != nil {
		return err
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	frame, err := wire.SealRegistration([]byte("synthetic=app-attest-check&t=0"), serverKey.PublicKey().Bytes())
	if err != nil {
		return err
	}
	body, err := attestor.RegistrationBody(ctx, frame)
	if err != nil {
		return err
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return err
	}
	decodedFrame, err := base64.URLEncoding.DecodeString(form.Get("ENC"))
	if err != nil || !bytes.Equal(frame, decodedFrame) {
		return errors.New("ENC round trip failed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(form.Get("H"))
	if err != nil {
		return err
	}
	var proof struct{ Assertion []byte }
	if err := json.Unmarshal(payload, &proof); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		FrameBytes         int  `json:"frame_bytes"`
		AssertionBytes     int  `json:"assertion_bytes"`
		AuthorizationBytes int  `json:"authorization_bytes"`
		ContextBound       bool `json:"context_bound"`
	}{len(frame), len(proof.Assertion), len(authorization), true})
}

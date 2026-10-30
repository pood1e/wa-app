package wire

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAndroidRegistrationEnvelopeWithoutDeviceSignature(t *testing.T) {
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"reg_onboard_abprop", "exist", "code", "register"} {
		t.Run(route, func(t *testing.T) {
			const plaintext = "synthetic=registration"
			wantH := route != "reg_onboard_abprop"
			requests := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- struct{}{}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if got := r.PostForm.Has("H"); got != wantH {
					t.Errorf("H field present = %t, want %t", got, wantH)
				}
				if r.PostForm.Get("H") != "" || r.Header.Get("Authorization") != "" {
					t.Error("unavailable device signature produced proof material")
				}
				frame, err := base64.RawURLEncoding.DecodeString(r.PostForm.Get("ENC"))
				if err != nil || len(frame) != 32+len(plaintext)+16 {
					t.Error("invalid registration ENC frame")
				}
				wantFields := 1
				if wantH {
					wantFields++
				}
				if len(r.PostForm) != wantFields {
					t.Errorf("outer form field count = %d, want %d", len(r.PostForm), wantFields)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			t.Cleanup(server.Close)
			client := &NativeHTTPClient{DialTLSContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			endpoint := "https://registration.invalid/v2/" + route
			var result map[string]any
			if wantH {
				result, _, err = client.PostWASafe(t.Context(), endpoint, plaintext, "synthetic", serverKey.PublicKey().Bytes())
			} else {
				result, err = client.PostWASafeNoAuth(t.Context(), endpoint, plaintext, "synthetic", serverKey.PublicKey().Bytes())
			}
			if err != nil {
				t.Fatal(err)
			}
			if result["status"] != "ok" || len(requests) != 1 {
				t.Fatal("registration request was not accepted by the synthetic receiver")
			}
		})
	}
}

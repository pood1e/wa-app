package ios

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
)

func TestPasswordKeepsIOSFirstFactorKeysAcrossCooldownAndUnknownResponse(t *testing.T) {
	key, private := protocoltest.PasswordKey(t)
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const password = " Password1 "
	sealed, err := security.SealPassword(password, key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	login := syntheticPhone().GetCountryCallingCode() + syntheticPhone().GetNationalNumber()
	var first url.Values
	codes, passwords := 0, 0
	client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
			return nil, err
		}
		if request.PostForm.Has("H") || request.Header.Get("Authorization") != "" {
			t.Error("unsigned password flow called for device proof")
		}
		frame, err := base64.URLEncoding.DecodeString(request.PostForm.Get("ENC"))
		if err != nil {
			t.Error(err)
			return nil, err
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
		if err != nil {
			t.Error(err)
			return nil, err
		}
		if first == nil {
			first = query
		}
		for _, name := range []string{"authkey", "e_ident", "e_skey_val", "id", "fdid", "expid", "ios_id", "access_session_id"} {
			if first.Get(name) == "" || first.Get(name) != query.Get(name) {
				t.Errorf("password verification replaced %s", name)
			}
		}
		switch request.URL.Path {
		case "/v2/exist":
			return jsonResponse(`{"status":"fail","reason":"incorrect","sms_wait":0}`), nil
		case "/v2/code":
			codes++
			return jsonResponse(`{"status":"sent","method":"sms","length":6}`), nil
		case "/v2/register":
			if query.Get("method") != "password" {
				return jsonResponse(`{"status":"fail","reason":"security_code","guess_wait":90}`), nil
			}
			passwords++
			text, err := protocoltest.OpenPassword(private, query.Get("code"))
			if err != nil || text != password || query.Get("context") != "twofac_dynamic" || query.Has("entered") || query.Has("security_code") {
				t.Error("iOS two-factor password fields differ from the native constructor")
			}
			if passwords == 1 {
				return jsonResponse(`{"status":"fail","reason":"incorrect","guess_wait":120}`), nil
			}
			if passwords == 2 {
				return nil, io.ErrUnexpectedEOF
			}
			return jsonResponse(`{"status":"ok","login":"` + login + `","lid":"synthetic-lid"}`), nil
		default:
			return nil, errors.New("unexpected verification route")
		}
	})}
	flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), client, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close(t.Context()) })
	flow.http.serverKey = serverKey.PublicKey().Bytes()
	if _, err = flow.SubmitPassword(t.Context(), sealed); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("password was submitted without confirmed first-factor verification")
	}
	if _, err = flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.RequestCode(t.Context(), smsMethod); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.SubmitCode(t.Context(), smsMethod, "123456"); err != nil {
		t.Fatal(err)
	}
	if required, _ := flow.PasswordChallenge(); !required {
		t.Fatal("iOS security_code lost the password step")
	}
	if _, err = flow.Material(login, login); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("unverified password allowed account handoff")
	}
	if _, err = flow.SubmitPassword(t.Context(), sealed); !errors.Is(err, ErrPasswordCooldown) || passwords != 0 {
		t.Fatal("first-factor password cooldown made a remote request")
	}
	flow.passwordRetryAt = time.Now().Add(-time.Second)
	if _, err = flow.SubmitPassword(t.Context(), sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.SubmitPassword(t.Context(), sealed); !errors.Is(err, ErrPasswordCooldown) || passwords != 1 {
		t.Fatal("password cooldown repeated a request")
	}
	flow.passwordRetryAt = time.Now().Add(-time.Second)
	if _, err = flow.SubmitPassword(t.Context(), sealed); err == nil || !flow.OutcomeUnknown() {
		t.Fatal("interrupted password verification lost its unknown outcome")
	}
	if _, err = flow.Material(login, login); err != nil {
		t.Fatal("uncertain password verification lost its recovery keys")
	}
	if _, err = flow.SubmitPassword(t.Context(), sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Material(login, "synthetic-lid"); err != nil || codes != 1 || passwords != 3 {
		t.Fatal("password resubmission changed the original verification flow")
	}
}

func TestExpiredIOSVerificationCanRequestANewCode(t *testing.T) {
	for _, passwordStep := range []bool{false, true} {
		t.Run(map[bool]string{false: "code", true: "password"}[passwordStep], func(t *testing.T) {
			codes, registrations := 0, 0
			client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/v2/exist":
					return jsonResponse(`{"status":"fail","reason":"incorrect","sms_wait":0}`), nil
				case "/v2/code":
					codes++
					return jsonResponse(`{"status":"sent","method":"sms","length":6}`), nil
				default:
					registrations++
					if passwordStep && registrations == 1 {
						return jsonResponse(`{"status":"fail","reason":"security_code"}`), nil
					}
					return jsonResponse(`{"status":"fail","reason":"stale"}`), nil
				}
			})}
			flow, err := Begin(t.Context(), syntheticPhone(), syntheticProfile(), client, Dependencies{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = flow.Close(t.Context()) })
			if _, err = flow.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = flow.RequestCode(t.Context(), smsMethod); err != nil {
				t.Fatal(err)
			}
			if _, err = flow.SubmitCode(t.Context(), smsMethod, "123456"); err != nil {
				t.Fatal(err)
			}
			if passwordStep {
				if _, err = flow.SubmitPassword(t.Context(), "#PWD_WA:11:synthetic"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = flow.SubmitCode(t.Context(), smsMethod, "123456"); !errors.Is(err, ErrRegistrationState) {
				t.Fatal("expired first factor accepted another verification submission")
			}
			if required, _ := flow.PasswordChallenge(); required {
				t.Fatal("expired first factor retained the password step")
			}
			if _, err = flow.RequestCode(t.Context(), smsMethod); err != nil || codes != 2 {
				t.Fatal("expired verification did not return to code request")
			}
		})
	}
}

package ios

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type registrationTransport func(*http.Request) (*http.Response, error)

func (f registrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestHTTPLogsSanitizedRegistrationRejection(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	client := &http.Client{Transport: registrationTransport(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"status":"fail","reason":"blocked","login":"PRIVATE_LOGIN_SENTINEL"}`), nil
	})}
	flow, _, _ := beginSyntheticFlow(t, client)
	response, err := flow.Probe(t.Context())
	if err != nil || response.GetReason() != "blocked" {
		t.Fatal("logging changed the protocol response")
	}
	if !strings.Contains(output.String(), "kind=exist") || !strings.Contains(output.String(), "reason=blocked") || strings.Contains(output.String(), "PRIVATE_") {
		t.Fatal("iOS rejection was not diagnosed safely")
	}
}

func TestFlowDoesNotReplaySMSAfterUnknownTransportResult(t *testing.T) {
	var sends atomic.Int32
	client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v2/exist" {
			return jsonResponse(`{"status":"fail","reason":"incorrect"}`), nil
		}
		sends.Add(1)
		return nil, io.ErrUnexpectedEOF
	})}
	flow, _, _ := beginSyntheticFlow(t, client)
	if _, err := flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() { _, err := flow.RequestCode(t.Context(), smsMethod); results <- err })
	}
	group.Wait()
	close(results)
	var uncertain, rejected int
	for err := range results {
		if errors.Is(err, ErrRegistrationState) {
			rejected++
		} else if errors.Is(err, io.ErrUnexpectedEOF) {
			uncertain++
		}
	}
	if sends.Load() != 1 || uncertain != 1 || rejected != 1 {
		t.Fatal("concurrent requests replayed an SMS with unknown delivery result")
	}
	if _, err := flow.SubmitCode(t.Context(), smsMethod, "123456"); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("unknown SMS result permitted verification")
	}
}

func TestHTTPRejectsRedirectWithoutForwardingProof(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusTemporaryRedirect,
			Header: http.Header{"Location": {"https://elsewhere.invalid/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	flow, _, _ := beginSyntheticFlow(t, client)
	_, err := flow.Probe(t.Context())
	var upstream *HTTPStatusError
	if !errors.As(err, &upstream) || upstream.StatusCode != 307 || calls.Load() != 1 {
		t.Fatal("redirect forwarded the registration proof")
	}
}

func TestHTTPBoundsAndRedactsMalformedResponses(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"malformed JSON", `{"status":"PRIVATE_SENTINEL"`},
		{"missing status", `{"login":"PRIVATE_SENTINEL"}`},
		{"oversized body", `{"status":"ok","unknown":"` + strings.Repeat("x", maxRegistrationResponse) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: registrationTransport(func(*http.Request) (*http.Response, error) {
				return jsonResponse(test.body), nil
			})}
			flow, _, _ := beginSyntheticFlow(t, client)
			if _, err := flow.Probe(t.Context()); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("malformed response was accepted or leaked its contents")
			}
		})
	}
}

func TestFlowHonorsServerSMSCooldown(t *testing.T) {
	for _, test := range []struct {
		name        string
		probe, code string
	}{
		{"probe cooldown", `{"status":"fail","reason":"incorrect","sms_wait":60}`, ""},
		{"unavailable SMS", `{"status":"fail","reason":"incorrect","sms_wait":-1}`, ""},
		{"rejected code request", `{"status":"fail","reason":"incorrect","sms_wait":0}`, `{"status":"fail","reason":"too_recent","retry_after":60}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/v2/exist" {
					return jsonResponse(test.probe), nil
				}
				sends.Add(1)
				return jsonResponse(test.code), nil
			})}
			flow, _, _ := beginSyntheticFlow(t, client)
			if _, err := flow.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			if test.code != "" {
				result, err := flow.RequestCode(t.Context(), smsMethod)
				if err != nil || result.GetReason() != "too_recent" {
					t.Fatal("server rejection was not preserved")
				}
			}
			before := sends.Load()
			if _, err := flow.RequestCode(t.Context(), smsMethod); !errors.Is(err, ErrCodeCooldown) || sends.Load() != before {
				t.Fatal("cooling or unavailable SMS channel performed another delivery request")
			}
		})
	}
}

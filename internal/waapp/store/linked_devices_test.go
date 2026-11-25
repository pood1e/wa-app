package store_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type linkedDeviceFixture struct {
	logoutConnection
	calls       atomic.Int32
	linkStarted chan struct{}
	allowLink   chan struct{}
	unknown     bool
}

func (f *linkedDeviceFixture) ListLinkedDevices(context.Context) ([]*waappv1.LinkedDevice, error) {
	return []*waappv1.LinkedDevice{{DeviceRef: "12025550123:2@s.whatsapp.net", Name: "Fixture browser"}}, nil
}
func (f *linkedDeviceFixture) UnlinkDevice(context.Context, string) error { return nil }
func (f *linkedDeviceFixture) LinkDevice(ctx context.Context, _ string) (*waappv1.LinkDeviceResponse, error) {
	f.calls.Add(1)
	if f.linkStarted != nil {
		close(f.linkStarted)
		select {
		case <-f.allowLink:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.unknown {
		return &waappv1.LinkDeviceResponse{OutcomeUnknown: true}, errors.New("synthetic interrupted response")
	}
	return &waappv1.LinkDeviceResponse{Device: &waappv1.LinkedDevice{DeviceRef: "12025550123:2@s.whatsapp.net", Name: "Fixture browser"}}, nil
}

func linkedDeviceApp(t *testing.T, repo repository, fixture *linkedDeviceFixture) (*account.Application, *waappv1.LinkDeviceRequest) {
	t.Helper()
	ephemeral := runtime.NewMemoryRuntime()
	t.Cleanup(func() { _ = ephemeral.Close() })
	fixture.started = make(chan string, 1)
	app := account.New(repo, ephemeral, fixture, func(context.Context, wacore.EngineMessageInput) (wacore.Communication, error) { return fixture, nil }, nil, nil)
	material := testMaterial(t)
	accepted, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "linked-device-fixture"}, Material: material})
	if err != nil || accepted.GetError() != nil {
		t.Fatal("seed linked-device account failed", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = app.RunLongConnections(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-fixture.started:
	case <-time.After(5 * time.Second):
		t.Fatal("linked-device account connection did not start")
	}
	qr := strings.Join([]string{"synthetic-pairing-reference", wire.B64u(material.GetCredentials().GetChatStatic().GetPublic()), wire.B64u(material.GetCredentials().GetIdentity().GetPublic()), wire.B64u(bytes.Repeat([]byte{7}, 32)), "1"}, ",")
	return app, &waappv1.LinkDeviceRequest{Context: &waappv1.RequestContext{RequestId: "link-fixture"}, WaAccountId: accepted.GetAccount().GetWaAccountId(), QrCode: &waappv1.SensitiveText{Value: qr}}
}

func TestLinkedDeviceAuthorizationIsIdempotentAndAccountScoped(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			fixture := new(linkedDeviceFixture)
			app, request := linkedDeviceApp(t, repo, fixture)
			for attempt := range 2 {
				response, err := app.LinkDevice(t.Context(), request)
				if err != nil || response.GetError() != nil || response.GetDevice().GetName() != "Fixture browser" {
					t.Fatal("same authorization changed its acknowledged result", err)
				}
				response.Device.Name = "changed caller result"
				if attempt == 0 {
					request.QrCode.Value = "https://wa.me/settings/linked_devices#" + request.QrCode.GetValue()
				}
			}
			if fixture.calls.Load() != 1 {
				t.Fatal("equivalent QR representations replayed the authorization")
			}
			request.WaAccountId = "waid_missing"
			response, _ := app.LinkDevice(t.Context(), request)
			if response.GetError() == nil || response.GetDevice() != nil || fixture.calls.Load() != 1 {
				t.Fatal("cached authorization bypassed account ownership")
			}
		})
	}
}

func TestConcurrentAndUncertainDeviceLinksAreNotReplayed(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent", true: "uncertain"}[uncertain], func(t *testing.T) {
			repo, _ := openRepository(t, "sqlite")
			fixture := &linkedDeviceFixture{unknown: uncertain, linkStarted: make(chan struct{}), allowLink: make(chan struct{})}
			app, request := linkedDeviceApp(t, repo, fixture)
			done := make(chan *waappv1.LinkDeviceResponse, 1)
			go func() { response, _ := app.LinkDevice(t.Context(), request); done <- response }()
			<-fixture.linkStarted
			response, _ := app.LinkDevice(t.Context(), request)
			if !response.GetOutcomeUnknown() || response.GetError() == nil || fixture.calls.Load() != 1 {
				t.Fatal("concurrent authorization was replayed")
			}
			close(fixture.allowLink)
			first := <-done
			response, _ = app.LinkDevice(t.Context(), request)
			if first.GetOutcomeUnknown() != uncertain || response.GetOutcomeUnknown() != uncertain || fixture.calls.Load() != 1 {
				t.Fatal("uncertain or acknowledged authorization was replayed")
			}
		})
	}
}

package communication

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type pictureLocationSender struct{ calls int }

func (s *pictureLocationSender) SendIQ(context.Context, state.NativeState, string, string, wire.ChatdNode, string) (wire.ChatdNode, ChatdSessionUpdate, error) {
	s.calls++
	return wire.ChatdNode{Tag: "iq", Content: []wire.ChatdNode{{Tag: "picture", Attrs: map[string]string{"id": "42", "url": "https://pps.whatsapp.net/fixture.jpg"}}}}, ChatdSessionUpdate{}, nil
}

type pictureIDs struct{}

func (pictureIDs) NewID(string) string { return "picture-request" }

type pictureTransport func(*http.Request) (*http.Response, error)

func (transport pictureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPictureDownloadDoesNotBlockProfileChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		engine.ids = pictureIDs{}
		started, release := make(chan struct{}), make(chan struct{})
		releaseDownload := sync.OnceFunc(func() { close(release) })
		defer releaseDownload()
		engine.http.Client = &http.Client{Transport: pictureTransport(func(request *http.Request) (*http.Response, error) {
			close(started)
			select {
			case <-request.Context().Done():
				return nil, request.Context().Err()
			case <-release:
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("fixture-image"))}, nil
			}
		})}
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		connection.installSession(receiveTestSession(t, wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "picture-request", "type": "result"}, Content: []wire.ChatdNode{{Tag: "picture", Attrs: map[string]string{"id": "42", "url": "https://pps.whatsapp.net/fixture.jpg"}}}}, &receiveTestClock{now: time.Now()}))
		done := make(chan wacore.EngineContactProfilePictureResult, 1)
		go func() {
			done <- connection.ResolveContactProfilePicture(t.Context(), wacore.EngineContactProfilePictureInput{WAAccountID: "account", ClientProfileID: "current", RegisteredIdentityID: "identity", ContactJID: "12025550124@s.whatsapp.net"})
		}()
		<-started
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		result := connection.ApplyAccountSettings(ctx, wacore.EngineAccountSettingsInput{ClientProfileID: "current", Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_NAME_SET, DisplayName: "Updated profile"})
		if result.Err != nil {
			t.Fatal("profile change waited for the blocked image download", result.Err)
		}
		releaseDownload()
		if picture := <-done; picture.Err != nil || string(picture.Data) != "fixture-image" {
			t.Fatal("releasing protocol admission interrupted the independent image download", picture.Err)
		}
		current, err := engine.loadState(t.Context(), "current")
		if err != nil || current.PushName != "Updated profile" {
			t.Fatal("completed image download overwrote the profile change")
		}
	})
}

func TestProfilePictureStopsAfterFirstUsableLocation(t *testing.T) {
	service := &contactsService{&engineCore{clock: shared.SystemClock{}, ids: shared.RandomIDGenerator{}}}
	sender := new(pictureLocationSender)
	locations, _, err := service.ContactProfilePictureLocationsFromProfileIQ(t.Context(), sender, state.NativeState{}, wacore.EngineContactProfilePictureInput{ContactPNJID: "12025550124@s.whatsapp.net", ContactPictureID: "42"}, "123456789@lid")
	if err != nil || len(locations) != 1 || sender.calls != 1 {
		t.Fatal("a valid image location triggered additional queries before downloading", err)
	}
}

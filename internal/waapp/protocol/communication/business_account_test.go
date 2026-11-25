package communication

import (
	"bytes"
	"context"
	"testing"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"google.golang.org/protobuf/proto"
)

type businessSender func(wire.ChatdNode) (wire.ChatdNode, error)

func (sender businessSender) SendIQ(_ context.Context, _ state.NativeState, _, _ string, request wire.ChatdNode, _ string) (wire.ChatdNode, ChatdSessionUpdate, error) {
	response, err := sender(request)
	return response, ChatdSessionUpdate{}, err
}

func businessCertificateResponse(data []byte) wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}, Content: []wire.ChatdNode{{Tag: "verified_name", Attrs: map[string]string{"v": "1", "verified_level": "low"}, Content: data}}}
}

func businessMissingResponse() wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "error"}, Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "404"}}}}
}

func TestBusinessNameConfirmsRemoteValueAndRetainsAccountKeys(t *testing.T) {
	for _, initial := range []string{"missing", "empty bootstrap", "previous identity"} {
		t.Run(initial, func(t *testing.T) { testBusinessNameConfirmation(t, initial) })
	}
}

func testBusinessNameConfirmation(t *testing.T, initial string) {
	t.Helper()
	current := newMessageParty(t)
	current.Client.AccountType = modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS
	engine, repository := newHistoricalMessageEngine(t, current, newMessageParty(t))
	keys := bytes.Clone(repository.states["current"].credentials)
	unrelated := bytes.Clone(repository.states["previous"].recovery)
	public, _ := wire.DecodeB64Any(current.Signal.Identity.Public)
	var certificate []byte
	if initial != "missing" {
		party, name := current, ""
		if initial == "previous identity" {
			party, name = newMessageParty(t), "WA Lab"
		}
		private, _ := wire.DecodeB64Any(party.Signal.Identity.Private)
		var err error
		certificate, err = wire.NewBusinessCertificate(private, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	var reads, writes int
	sender := businessSender(func(request wire.ChatdNode) (wire.ChatdNode, error) {
		if request.Attrs["xmlns"] != "w:biz" || request.Attrs["to"] != "s.whatsapp.net" {
			t.Fatal("business name update changed IQ routing")
		}
		name, _ := ChatdChild(request, "verified_name")
		if request.Attrs["type"] == "get" {
			reads++
			if len(certificate) == 0 {
				return businessMissingResponse(), nil
			}
			return businessCertificateResponse(certificate), nil
		}
		writes++
		certificate = bytes.Clone(name.Content.([]byte))
		var signed waVnameCert.VerifiedNameCertificate
		if proto.Unmarshal(certificate, &signed) != nil || name.Attrs["v"] != "2" {
			t.Fatal("business certificate schema differs from the native request")
		}
		valid, err := wire.XeddsaVerifyCurve25519(public, signed.GetDetails(), signed.GetSignature())
		if err != nil || !valid {
			t.Fatal("business name update rotated the identity key")
		}
		return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}, Content: []wire.ChatdNode{{Tag: "verified_name", Attrs: map[string]string{"id": "synthetic-certificate-id"}}}}, nil
	})
	input := wacore.EngineAccountSettingsInput{ClientProfileID: "current", RegisteredIdentityID: "12025550123@s.whatsapp.net", Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_BUSINESS_NAME_SET, DisplayName: "WA Lab"}
	for range 2 {
		loaded, err := engine.loadState(t.Context(), "current")
		if err != nil {
			t.Fatal(err)
		}
		result := engine.ApplyAccountSettingsWithSender(t.Context(), input, loaded, sender)
		if result.Err != nil || result.Status != waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED {
			t.Fatal("business name update failed", result.Err)
		}
	}
	loaded, err := engine.loadState(t.Context(), "current")
	if err != nil || loaded.Client.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS || !bytes.Equal(loaded.BusinessIdentity.GetCertificate(), certificate) || !bytes.Equal(keys, repository.states["current"].credentials) || !bytes.Equal(unrelated, repository.states["previous"].recovery) || writes != 1 || reads != 3 {
		t.Fatal("conversion or retry replaced existing keys, changed another account, or resubmitted the certificate")
	}
}

func TestBusinessNameRequiresCompleteRemoteConfirmation(t *testing.T) {
	for _, failure := range []string{"reject", "missing ack", "missing readback", "malformed readback", "wrong identity"} {
		t.Run(failure, func(t *testing.T) {
			current := newMessageParty(t)
			current.Client.AccountType = modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS
			engine, repository := newHistoricalMessageEngine(t, current, newMessageParty(t))
			before := bytes.Clone(repository.states["current"].recovery)
			wrote := false
			sender := businessSender(func(request wire.ChatdNode) (wire.ChatdNode, error) {
				if request.Attrs["type"] == "set" {
					wrote = true
					if failure == "reject" {
						return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "error"}, Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "403"}}}}, nil
					}
					if failure == "missing ack" {
						return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}}, nil
					}
					return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result"}, Content: []wire.ChatdNode{{Tag: "verified_name", Attrs: map[string]string{"id": "synthetic"}}}}, nil
				}
				if wrote && failure == "malformed readback" {
					return businessCertificateResponse([]byte("invalid")), nil
				}
				if wrote && failure == "wrong identity" {
					other := newMessageParty(t)
					private, _ := wire.DecodeB64Any(other.Signal.Identity.Private)
					certificate, err := wire.NewBusinessCertificate(private, "WA Lab")
					if err != nil {
						t.Fatal(err)
					}
					return businessCertificateResponse(certificate), nil
				}
				return businessMissingResponse(), nil
			})
			input := wacore.EngineAccountSettingsInput{ClientProfileID: "current", RegisteredIdentityID: "12025550123@s.whatsapp.net", Kind: waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_BUSINESS_NAME_SET, DisplayName: "WA Lab"}
			result := engine.ApplyAccountSettingsWithSender(t.Context(), input, current, sender)
			if result.Err == nil || result.Status == waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED || !bytes.Equal(before, repository.states["current"].recovery) {
				t.Fatal("an incomplete conversion changed the durable account type")
			}
		})
	}
}

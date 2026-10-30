package state_test

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func TestPQCredentialsAndConnectionSurviveRecovery(t *testing.T) {
	profile := protocoltest.AndroidBuild(t)
	parameters := protocoltest.AndroidClient()
	parameters.Options.EnablePqRegistration = true
	parameters.Options.HandshakePqMode = protocolv1.HandshakePQMode_IKKEM_FS
	phone := &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}
	flow, err := onboarding.NewAndroidFlow(t.Context(), phone, profile, parameters, "", onboarding.AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	material, err := flow.Material("12025550123", "synthetic-account")
	if err != nil {
		t.Fatal(err)
	}
	material.Recovery.ChatConnection = &protocolv1.ChatConnectionState{ServerStaticPublic: "synthetic-server-key", ServerStaticPqPublic: bytes.Repeat([]byte{7}, 800)}
	material.Credentials.Canonical = &protocolv1.CanonicalCredentials{UserId: "9001", AccessToken: "synthetic-canonical-token", CredentialPassword: "synthetic-credential-password", ExpiresAt: 1800000000}
	material.Recovery.AccountReview = &protocolv1.AccountReviewState{Token: "synthetic-review-token", Review: &protocolv1.AccountReview{Status: protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW, CheckedAt: timestamppb.Now()}}
	restored, err := state.Restore(material.GetCredentials(), material.GetRecovery())
	if err != nil {
		t.Fatal(err)
	}
	keys, recovery, err := restored.Export()
	if err != nil || !proto.Equal(keys, material.GetCredentials()) || !proto.Equal(recovery, material.GetRecovery()) {
		t.Fatal("PQ private material or connection recovery state changed")
	}
	if restored.AuthKey != "" || restored.Profile.FDID != "" {
		t.Fatal("device preparation survived account handoff")
	}
	restored.Canonical.AccessToken = "changed-synthetic-token"
	if keys.GetCanonical().GetAccessToken() != "synthetic-canonical-token" || material.GetCredentials().GetCanonical().GetAccessToken() != "synthetic-canonical-token" {
		t.Fatal("canonical recovery retained caller-owned authorization")
	}
	restored.AccountReview.Token = "changed-synthetic-token"
	if recovery.GetAccountReview().GetToken() != "synthetic-review-token" || material.GetRecovery().GetAccountReview().GetToken() != "synthetic-review-token" {
		t.Fatal("review recovery retained caller-owned authorization")
	}
	restored.Signal.PQLastResortPreKey.Private[0] ^= 1
	if bytes.Equal(restored.Signal.PQLastResortPreKey.GetPrivate(), keys.GetPqLastResortPreKey().GetPrivate()) {
		t.Fatal("recovery retained caller-owned PQ private bytes")
	}
}

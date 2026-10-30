package communication_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type reviewStateRepository struct {
	mu          sync.Mutex
	credentials *protocolv1.AccountCredentials
	recovery    *protocolv1.AccountRecoveryState
}

func (*reviewStateRepository) ListRetiredProtocolProfiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (r *reviewStateRepository) GetProtocolState(context.Context, string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return proto.Clone(r.credentials).(*protocolv1.AccountCredentials), proto.Clone(r.recovery).(*protocolv1.AccountRecoveryState), nil
}

func (r *reviewStateRepository) SaveProtocolState(_ context.Context, _ string, keys *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials = proto.Clone(keys).(*protocolv1.AccountCredentials)
	r.recovery = proto.Clone(recovery).(*protocolv1.AccountRecoveryState)
	return nil
}

type reviewTransport func(*http.Request) (*http.Response, error)

func (f reviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAmbiguousReviewSubmissionSurvivesRestartWithoutResubmission(t *testing.T) {
	flow, err := onboarding.NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}, protocoltest.AndroidBuild(t), protocoltest.AndroidClient(), "", onboarding.AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()
	material, err := flow.Material("12025550123", "synthetic-account")
	if err != nil {
		t.Fatal(err)
	}
	material.Recovery.AccountReview = &protocolv1.AccountReviewState{Token: "synthetic-private-review-token"}
	repository := &reviewStateRepository{credentials: material.GetCredentials(), recovery: material.GetRecovery()}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.URLEncoding.EncodeToString(serverKey.PublicKey().Bytes())
	keysJSON, err := json.Marshal(struct {
		Identity  string `json:"purpose_public_ik"`
		Ephemeral string `json:"purpose_public_ek"`
		Nonce     string `json:"purpose_dummy_nonce"`
	}{public, public, base64.URLEncoding.EncodeToString(make([]byte, 24))})
	if err != nil {
		t.Fatal(err)
	}
	phase, submissions := "NO_APPEAL_OPENED", 0
	transport := reviewTransport(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			Document int64 `json:"doc_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		body := ""
		switch payload.Document {
		case 0xf3123d2188cf7:
			body = `{"data":{"whatsapp_support_ban_appeal_status":{"status":"` + phase + `"}}}`
		case 0x149e09feba5e1b:
			body = `{"data":{"purpose_public_keys":` + string(keysJSON) + `}}`
		case 0x1371f3dd6320f6:
			submissions++
			return nil, context.DeadlineExceeded
		default:
			t.Error("unexpected upstream request")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	newEngine := func() *communication.Engine {
		t.Helper()
		engine, err := communication.New(repository, "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(engine.CloseIdleConnections)
		client, finish, err := engine.HTTPForProfile(t.Context(), "synthetic-profile")
		if err != nil {
			t.Fatal(err)
		}
		client.Client.Transport = transport
		t.Cleanup(finish)
		return engine
	}
	engine := newEngine()
	result, err := engine.SubmitAccountReview(t.Context(), "synthetic-profile", "synthetic review description")
	if err == nil || shared.ToProtoError(err).GetRetryable() || result.GetStatus() != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN || submissions != 1 {
		t.Fatal("ambiguous submission was retried or reported as accepted")
	}
	engine = newEngine()
	result, err = engine.GetAccountReview(t.Context(), "synthetic-profile", false)
	if err != nil || result.GetStatus() != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_SUBMISSION_UNKNOWN {
		t.Fatal("restart lost the uncertain submission state")
	}
	phase = "IN_REVIEW"
	result, err = engine.SubmitAccountReview(t.Context(), "synthetic-profile", "a second synthetic description")
	if err != nil || result.GetStatus() != protocolv1.AccountReviewStatus_ACCOUNT_REVIEW_STATUS_IN_REVIEW || submissions != 1 {
		t.Fatal("status reconciliation opened a duplicate review")
	}
	keys, recovery, err := repository.GetProtocolState(t.Context(), "synthetic-profile")
	if err != nil || !proto.Equal(keys, material.GetCredentials()) || recovery.GetAccountReview().GetToken() != "synthetic-private-review-token" {
		t.Fatal("review changed account credentials or lost authorization")
	}
}

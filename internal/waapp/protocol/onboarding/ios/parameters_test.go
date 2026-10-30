package ios

import (
	"bytes"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestPlaintextMatchesNativeFoundationQuery(t *testing.T) {
	query := parameters{{"sample", `AZ09-._~ /?+&=:@!$'()*;,[]`}}
	want := `sample=AZ09-._~ /?%2B&=:@!$'()*;,[]`
	if got := string(query.plaintext()); got != want {
		t.Fatalf("Foundation query got %q, want %q", got, want)
	}
	if got := percentBytes([]byte{0, '+', 255}); got != "%00%2B%FF" {
		t.Fatalf("recovery identifier got %q, want %%00%%2B%%FF", got)
	}
}

func TestRegistrationParametersRemainStableWithoutSharingFlows(t *testing.T) {
	keys, err := wire.NewRegistrationKeys()
	if err != nil {
		t.Fatal(err)
	}
	params, err := newRegistrationParameters(syntheticPhone(), syntheticProfile(), keys, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	other, err := newRegistrationParameters(syntheticPhone(), syntheticProfile(), keys, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	before := params.request("exist", "", "", time.Unix(2, 0)).plaintext()
	if params.id == other.id || params.iosID == other.iosID || len(params.id) != 48 {
		t.Fatal("flows shared registration identifiers")
	}
	_ = params.request("code", "sms", "", time.Unix(3, 0))
	_ = params.request("register", "", "123456", time.Unix(4, 0))
	if !bytes.Equal(before, params.request("exist", "", "", time.Unix(2, 0)).plaintext()) {
		t.Fatal("later requests mutated the common registration parameters")
	}
}

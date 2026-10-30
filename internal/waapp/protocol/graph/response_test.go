package graph_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/beeper/argo-go/codec"
	"github.com/beeper/argo-go/header"
	"github.com/beeper/argo-go/wire"
	"github.com/elliotchance/orderedmap/v3"

	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
)

func TestNativeArgoPasswordStatus(t *testing.T) {
	wt, err := security.HasPassword.ResponseType()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString("040c000000000303")
	data, err := graph.Decode(raw, "argo", wt)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status *struct {
			Has     bool `json:"has_password"`
			Upgrade bool `json:"upgrade_needed"`
		} `json:"xwa2_has_password"`
	}
	if json.Unmarshal(data, &result) != nil || result.Status == nil || result.Status.Has || result.Status.Upgrade {
		t.Fatal("native Argo status did not decode")
	}
}

func TestGraphQLErrorsDoNotExposeBodies(t *testing.T) {
	for _, raw := range []string{
		`{"errors":[{"message":"synthetic-secret","extensions":{"error_code":400,"is_retryable":false}}]}`,
		`{"error":{"code":190,"message":"synthetic-secret"}}`,
		`{"data":null}`,
		`<html>synthetic-secret</html>`,
	} {
		if _, err := graph.Decode([]byte(raw), "json", nil); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid GraphQL response was accepted or exposed")
		}
	}
}

func TestGraphQLWithoutFormatUsesJSON(t *testing.T) {
	data, err := graph.Decode([]byte(`{"data":{"accepted":true}}`), "", nil)
	if err != nil || string(data) != `{"accepted":true}` {
		t.Fatal("the native default JSON format was interpreted as Argo", err)
	}
	_, err = graph.Decode([]byte(`{"errors":[{"extensions":{"error_code":400}}]}`), "", nil)
	var rejected graph.ResponseError
	if !errors.As(err, &rejected) || rejected.Extensions.Code != 400 {
		t.Fatal("JSON rejection was hidden by an Argo decoding failure")
	}
}

func TestNativeArgoErrorPathsAndExtensions(t *testing.T) {
	typ, err := security.GetCertificates.ResponseType()
	if err != nil {
		t.Fatal(err)
	}
	fixture := typ.(wire.RecordType)
	fixture.Fields = append([]wire.Field(nil), fixture.Fields...)
	fixture.Fields[1].Of = wire.ArrayType{Of: wire.RecordType{Fields: []wire.Field{
		{Name: "message", Of: wire.NewBlockType(wire.String, "String", true)},
		{Name: "locations", Of: wire.ArrayType{Of: wire.Location}, Omittable: true},
		{Name: "path", Of: wire.RecordType{Fields: []wire.Field{{Name: "segments", Of: wire.ArrayType{Of: wire.VarintBlock}}}}, Omittable: true},
		{Name: "extensions", Of: wire.Desc, Omittable: true},
	}}}
	fixture.Fields[2].Of = wire.Desc
	encoder := codec.NewArgoEncoder()
	encoder.Header().SetFlag(header.HeaderOutOfBandFieldErrorsFlag, true)
	value := map[string]any{
		"data": nil,
		"errors": []any{map[string]any{
			"message":    "synthetic-secret",
			"path":       map[string]any{"segments": []any{0}},
			"extensions": map[string]any{"error_code": 400, "is_retryable": false},
		}},
		"extensions": map[string]any{"synthetic_metadata": true},
	}
	if err = encoder.ValueToArgoWithType(orderedFixture(value), fixture); err != nil {
		t.Fatal(err)
	}
	raw, err := encoder.GetResult()
	if err != nil {
		t.Fatal(err)
	}
	_, err = graph.Decode(raw.Bytes(), "argo", typ)
	var rejection graph.ResponseError
	if !errors.As(err, &rejection) || rejection.Extensions.Code != 400 || graph.Retryable(err) || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("native Argo error code was hidden by an error-path or metadata decoding failure")
	}
}

func orderedFixture(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := orderedmap.NewOrderedMap[string, any]()
		for key, value := range value {
			result.Set(key, orderedFixture(value))
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = orderedFixture(item)
		}
		return result
	default:
		return value
	}
}

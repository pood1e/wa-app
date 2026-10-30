// Package security implements native account password and two-step verification operations.
package security

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/beeper/argo-go/typer"
	"github.com/beeper/argo-go/wire"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type Operation string

const (
	GetTwofa          Operation = "GetTwofaQuery"
	HasPassword       Operation = "HasPasswordQuery"
	SetTwofa          Operation = "SetTwofaMutation"
	GetCertificates   Operation = "WWWGetCertificates"
	CreateUser        Operation = "WWWCreateUser"
	CreateAccessToken Operation = "WWWCreateAccessToken"
	TriggerRecovery   Operation = "WWWTriggerAcountRecovery"
	ExchangeNonce     Operation = "WWWExchangeNonceForAccessToken"
)

//go:embed schema.graphql
var schemaText string

//go:embed operations.graphql
var operationsText string

var schemas = sync.OnceValues(func() (map[Operation]wire.Type, error) {
	schema, err := gqlparser.LoadSchema(&ast.Source{Input: schemaText})
	if err != nil {
		return nil, err
	}
	queries, queryErr := gqlparser.LoadQuery(schema, operationsText)
	if queryErr != nil {
		return nil, queryErr
	}
	result := make(map[Operation]wire.Type, len(queries.Operations))
	for _, op := range queries.Operations {
		t, err := typer.NewTyper(schema, queries, op.Name)
		if err != nil {
			return nil, err
		}
		data, err := t.DataWireType()
		if err != nil {
			return nil, err
		}
		result[Operation(op.Name)] = wire.RecordType{Fields: []wire.Field{
			{Name: "data", Of: wire.NewNullableType(data)},
			{Name: "errors", Of: wire.ArrayType{Of: wire.Error}, Omittable: true},
			{Name: "extensions", Of: wire.Extensions, Omittable: true},
		}}
	}
	return result, nil
})

func (op Operation) DocumentID() string {
	switch op {
	case GetTwofa:
		return "27608881388708005"
	case HasPassword:
		return "25379221318423478"
	case SetTwofa:
		return "28092515560345874"
	case GetCertificates:
		return "25094190163544446"
	case CreateUser:
		return "8548056818544135"
	case CreateAccessToken:
		return "7956082757804344"
	case TriggerRecovery:
		return "24896267680055094"
	case ExchangeNonce:
		return "25727187160203620"
	default:
		return ""
	}
}

func (op Operation) ResponseType() (wire.Type, error) {
	types, err := schemas()
	if err != nil {
		return nil, err
	}
	typ, ok := types[op]
	if !ok {
		return nil, fmt.Errorf("unsupported account security operation")
	}
	return typ, nil
}

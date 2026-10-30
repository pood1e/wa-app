package graph

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beeper/argo-go/codec"
	"github.com/beeper/argo-go/pkg/buf"
	"github.com/beeper/argo-go/wire"
	"github.com/elliotchance/orderedmap/v3"
)

type Envelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []ResponseError `json:"errors"`
	Error  *ResponseError  `json:"error"`
}

type ResponseError struct {
	Code       int `json:"code"`
	Extensions struct {
		Code      int  `json:"error_code"`
		Retryable bool `json:"is_retryable"`
	} `json:"extensions"`
}

func (e ResponseError) Error() string {
	code := e.Code
	if code == 0 {
		code = e.Extensions.Code
	}
	return fmt.Sprintf("GraphQL request rejected (code %d)", code)
}

func Decode(data []byte, format string, typ wire.Type) (json.RawMessage, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("invalid GraphQL response size")
	}
	if format == "argo" {
		decoder, err := codec.NewArgoDecoder(buf.NewBufReadonly(data))
		if err != nil {
			return nil, errors.New("invalid GraphQL Argo response")
		}
		value, err := decoder.ArgoToMap(argoDecoderType(typ))
		if err != nil {
			return nil, errors.New("invalid GraphQL Argo payload")
		}
		data, err = json.Marshal(jsonValue(value))
		if err != nil {
			return nil, errors.New("GraphQL response could not be decoded")
		}
	} else if format != "" && format != "json" {
		return nil, errors.New("unsupported GraphQL response format")
	}
	var result Envelope
	if json.Unmarshal(data, &result) != nil {
		return nil, errors.New("invalid GraphQL JSON response")
	}
	if result.Error != nil {
		return nil, *result.Error
	}
	if len(result.Errors) > 0 {
		return nil, result.Errors[0]
	}
	if len(result.Data) == 0 || string(result.Data) == "null" {
		return nil, errors.New("GraphQL response is missing data")
	}
	return result.Data, nil
}

func jsonValue(value any) any {
	switch value := value.(type) {
	case *orderedmap.OrderedMap[string, any]:
		result := make(map[string]any, value.Len())
		for key, value := range value.AllFromFront() {
			result[key] = jsonValue(value)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = jsonValue(item)
		}
		return result
	default:
		return value
	}
}

package graph

import "github.com/beeper/argo-go/wire"

// argoDecoderType lowers native specializations to the codec's supported wire types.
func argoDecoderType(typ wire.Type) wire.Type {
	switch value := typ.(type) {
	case wire.ExtensionsType:
		return wire.Desc
	case wire.PathType:
		// The record preserves PATH's non-labeled optional marker.
		return wire.RecordType{Fields: []wire.Field{{Name: "segments", Of: wire.ArrayType{Of: wire.VarintBlock}}}}
	case wire.RecordType:
		fields := append([]wire.Field(nil), value.Fields...)
		for i := range fields {
			fields[i].Of = argoDecoderType(fields[i].Of)
		}
		return wire.RecordType{Fields: fields}
	case wire.ArrayType:
		return wire.ArrayType{Of: argoDecoderType(value.Of)}
	case wire.NullableType:
		return wire.NewNullableType(argoDecoderType(value.Of))
	case wire.BlockType:
		return wire.NewBlockType(argoDecoderType(value.Of), value.Key, value.Dedupe)
	default:
		return typ
	}
}

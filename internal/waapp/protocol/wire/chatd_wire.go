package wire

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	WaNoisePrologue = "WA\x06\x03"
	WaRoutingPrefix = "ED\x00\x01"
)

type ChatdError struct{ Message string }

func (e ChatdError) Error() string { return e.Message }

func NewChatdError(format string, args ...any) error {
	return ChatdError{Message: fmt.Sprintf(format, args...)}
}

func ChatdInt24(value int) ([]byte, error) {
	if value < 0 || value >= 1<<24 {
		return nil, fmt.Errorf("24-bit length out of range: %d", value)
	}
	return []byte{byte(value >> 16), byte(value >> 8), byte(value)}, nil
}

func ChatdReadFrame(r *bufio.Reader, maxFrameBytes int) ([]byte, error) {
	var reader ChatdFrameReader
	return reader.Read(r, maxFrameBytes)
}

// ChatdFrameReader retains partial frames when a read deadline interrupts I/O.
type ChatdFrameReader struct {
	payload []byte
	offset  int
}

func (f *ChatdFrameReader) Read(r *bufio.Reader, maxFrameBytes int) ([]byte, error) {
	if f.payload == nil {
		header, err := r.Peek(3)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(header, []byte("GOA")) {
			return nil, NewChatdError("server returned GOA before/inside frame")
		}
		length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
		if maxFrameBytes > 0 && length > maxFrameBytes {
			return nil, NewChatdError("chatd frame too large: %d", length)
		}
		if _, err := r.Discard(3); err != nil {
			return nil, err
		}
		f.payload = make([]byte, length)
	}
	n, err := io.ReadFull(r, f.payload[f.offset:])
	f.offset += n
	if err != nil {
		return nil, err
	}
	payload := f.payload
	f.payload, f.offset = nil, 0
	return payload, nil
}

func ChatdWriteFrame(w *bufio.Writer, payload []byte) error {
	header, err := ChatdInt24(len(payload))
	if err != nil {
		return err
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}

func PbVarint(value uint64) []byte {
	out := make([]byte, 0, 10)
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			out = append(out, b|0x80)
			continue
		}
		out = append(out, b)
		return out
	}
}

func PbKey(fieldNo int, wireType int) []byte {
	return PbVarint(uint64(fieldNo<<3 | wireType))
}

func PbVarintField(fieldNo int, value uint64) []byte {
	out := PbKey(fieldNo, 0)
	out = append(out, PbVarint(value)...)
	return out
}

func PbBoolField(fieldNo int, value bool) []byte {
	if value {
		return PbVarintField(fieldNo, 1)
	}
	return PbVarintField(fieldNo, 0)
}

func PbBytesField(fieldNo int, value []byte) []byte {
	if value == nil {
		return nil
	}
	out := PbKey(fieldNo, 2)
	out = append(out, PbVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func PbStringField(fieldNo int, value string) []byte {
	if value == "" {
		return nil
	}
	return PbBytesField(fieldNo, []byte(value))
}

type PbValue struct {
	WireType int
	Varint   uint64
	Bytes    []byte
}

func ReadPBVarint(data []byte, pos int) (uint64, int, error) {
	if pos < 0 || pos > len(data) {
		return 0, pos, NewChatdError("invalid protobuf offset")
	}
	value, size := protowire.ConsumeVarint(data[pos:])
	if size < 0 {
		return 0, pos, protowire.ParseError(size)
	}
	return value, pos + size, nil
}

func ParsePBFields(data []byte) (map[int][]PbValue, error) {
	pos := 0
	result := map[int][]PbValue{}
	for pos < len(data) {
		key, next, err := ReadPBVarint(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		fieldNo := int(key >> 3)
		if key>>3 > uint64(protowire.MaxValidNumber) || fieldNo < 1 {
			return nil, NewChatdError("invalid protobuf field number")
		}
		wireType := int(key & 7)
		value := PbValue{WireType: wireType}
		switch wireType {
		case 0:
			v, next, err := ReadPBVarint(data, pos)
			if err != nil {
				return nil, err
			}
			pos = next
			value.Varint = v
		case 1:
			if pos+8 > len(data) {
				return nil, NewChatdError("truncated protobuf fixed64")
			}
			value.Bytes = append([]byte{}, data[pos:pos+8]...)
			pos += 8
		case 2:
			data, size := protowire.ConsumeBytes(data[pos:])
			if size < 0 {
				return nil, protowire.ParseError(size)
			}
			value.Bytes = append([]byte{}, data...)
			pos += size
		case 5:
			if pos+4 > len(data) {
				return nil, NewChatdError("truncated protobuf fixed32")
			}
			value.Bytes = append([]byte{}, data[pos:pos+4]...)
			pos += 4
		default:
			return nil, NewChatdError("unsupported protobuf wire type %d", wireType)
		}
		result[fieldNo] = append(result[fieldNo], value)
	}
	return result, nil
}

func FirstPBBytes(fields map[int][]PbValue, fieldNo int, label string) ([]byte, error) {
	for _, value := range fields[fieldNo] {
		if value.WireType == 2 {
			return append([]byte{}, value.Bytes...), nil
		}
	}
	return nil, NewChatdError("missing protobuf bytes/message field %d (%s)", fieldNo, label)
}

func FirstPBVarint(fields map[int][]PbValue, fieldNo int, label string, defaultValue *uint64) (uint64, error) {
	for _, value := range fields[fieldNo] {
		if value.WireType == 0 {
			return value.Varint, nil
		}
	}
	if defaultValue != nil {
		return *defaultValue, nil
	}
	return 0, NewChatdError("missing protobuf varint field %d (%s)", fieldNo, label)
}

func OptionalPBBytes(fields map[int][]PbValue, fieldNo int) []byte {
	for _, value := range fields[fieldNo] {
		if value.WireType == 2 {
			return append([]byte{}, value.Bytes...)
		}
	}
	return nil
}

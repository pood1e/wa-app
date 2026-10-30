package communication

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestSignalBodyPreservesProtobufEndingInPaddingByte(t *testing.T) {
	message := &waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("fixture@g.us"), AxolotlSenderKeyDistributionMessage: []byte{7, 1}}}
	body, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{body, padSignalMessage(body)} {
		decoded, err := DecodeSignalMessage(input)
		if err != nil || !proto.Equal(decoded, message) {
			t.Fatal("a valid protobuf byte was mistaken for transport padding")
		}
	}
}

package communication

import (
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *ChatdSession) authenticate() error {
	for range 32 {
		node, err := s.Transport.ReadNode()
		if err != nil {
			return wire.ChatdPhase("read chatd login confirmation", err)
		}
		update, _, err := s.ConsumeIncomingNode(wacore.EngineMessageInput{}, node, s.loginUpdate, time.Now())
		if err != nil {
			return err
		}
		s.loginUpdate = update
		if node.Tag == "success" {
			hint := wacore.WAContactHint{LIDJID: node.Attrs["lid"], PNJID: s.PhoneJID}.Normalized()
			if hint.Valid() {
				s.loginUpdate.ContactHints = wire.DedupeWAContactHints(append(s.loginUpdate.ContactHints, hint))
			}
			return nil
		}
	}
	return wire.NewChatdError("chatd login confirmation is missing")
}

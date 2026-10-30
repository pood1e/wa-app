package communication

import (
	"bytes"
	"errors"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func validateRemoteSignalIdentity(current *state.NativeState, target string, actual []byte) error {
	if saved := current.Signal.RemoteIdentities[SignalSenderKey(target)]; saved != "" {
		expected, err := wire.DecodeB64Any(saved)
		if err != nil {
			return errors.New("saved peer identity is invalid")
		}
		expected, err = wire.WithSignalCurvePrefix(expected)
		actual, actualErr := wire.WithSignalCurvePrefix(actual)
		if err != nil || actualErr != nil || !bytes.Equal(expected, actual) {
			return errors.New("peer Signal identity changed")
		}
	}
	return nil
}

func migrateSignalDeviceIdentity(current *state.NativeState, source, target string) error {
	if source == target {
		return nil
	}
	if _, _, exists := ExactSignalSession(current.Signal.Sessions, target); exists {
		return nil
	}
	oldKey, session, exists := ExactSignalSession(current.Signal.Sessions, source)
	if !exists {
		return nil
	}
	remote, err := wire.DecodeB64Any(session.RemoteIdentityPublic)
	if err != nil {
		return err
	}
	if err := validateRemoteSignalIdentity(current, target, remote); err != nil {
		return err
	}
	session.Sender = target
	current.Signal.Sessions[SignalSenderKey(target)] = session
	current.Signal.RemoteIdentities[SignalSenderKey(target)] = session.RemoteIdentityPublic
	delete(current.Signal.Sessions, oldKey)
	delete(current.Signal.RemoteIdentities, SignalSenderKey(source))
	return nil
}

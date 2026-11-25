package communication

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"slices"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func prepareLinkedDeviceKeys(current *state.NativeState, now time.Time) error {
	current.EnsureMaps()
	if current.LinkedDevices == nil {
		return errors.New("companion initialization requires a linked-device state")
	}
	if len(current.LinkedDevices.GetHistoryThreadSecret()) == 0 {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		current.LinkedDevices.HistoryThreadSecret = secret
	}
	if len(current.AppState.Keys) > 0 {
		_, _, err := SelectedNativeAppStateKey(current)
		return err
	}
	var signed waAdv.ADVSignedKeyIndexList
	var indexes waAdv.ADVKeyIndexList
	if proto.Unmarshal(current.LinkedDevices.GetSignedKeyIndexList(), &signed) != nil || proto.Unmarshal(signed.GetDetails(), &indexes) != nil || indexes.GetRawID() == 0 {
		return errors.New("companion initialization requires its reserved account identity")
	}
	id, data := make([]byte, 6), make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	if _, err := rand.Read(data); err != nil {
		return err
	}
	fingerprint, err := proto.Marshal(&waE2E.AppStateSyncKeyFingerprint{RawID: indexes.RawID, CurrentIndex: indexes.CurrentIndex, DeviceIndexes: indexes.GetValidIndexes()})
	if err != nil {
		return err
	}
	key := state.NativeAppStateKey{KeyID: wire.B64u(id), KeyData: wire.B64u(data), Fingerprint: wire.B64u(fingerprint), Timestamp: now.UnixMilli()}
	current.AppState.Keys[key.KeyID] = key
	return nil
}

func linkedDeviceKeyShare(current *state.NativeState) (*waE2E.Message, error) {
	ids := make([]string, 0, len(current.AppState.Keys))
	for id := range current.AppState.Keys {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	share := new(waE2E.AppStateSyncKeyShare)
	for _, id := range ids {
		key := current.AppState.Keys[id].Normalized()
		if !key.Valid() {
			return nil, errors.New("companion initialization found an invalid app-state key")
		}
		keyID, _ := wire.DecodeB64Any(key.KeyID)
		data, _ := wire.DecodeB64Any(key.KeyData)
		record := &waE2E.AppStateSyncKeyData{KeyData: data, Timestamp: proto.Int64(key.Timestamp)}
		if key.Fingerprint != "" {
			raw, err := wire.DecodeB64Any(key.Fingerprint)
			fingerprint := new(waE2E.AppStateSyncKeyFingerprint)
			if err != nil || proto.Unmarshal(raw, fingerprint) != nil {
				return nil, errors.New("companion initialization found an invalid app-state fingerprint")
			}
			record.Fingerprint = fingerprint
		}
		share.Keys = append(share.Keys, &waE2E.AppStateSyncKey{KeyID: &waE2E.AppStateSyncKeyId{KeyID: keyID}, KeyData: record})
	}
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE.Enum(), AppStateSyncKeyShare: share}}, nil
}

func linkedDeviceInitialMetadata(current *state.NativeState) (*waE2E.Message, error) {
	history := &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_INITIAL_BOOTSTRAP.Enum(), ChunkOrder: proto.Uint32(0), Progress: proto.Uint32(100),
		ThreadIDUserSecret: bytes.Clone(current.LinkedDevices.GetHistoryThreadSecret()), ThreadDsTimeframeOffset: proto.Uint32(0)}
	for _, hint := range current.ContactHints {
		if hint.PNJID != "" && hint.LIDJID != "" {
			history.PhoneNumberToLidMappings = append(history.PhoneNumberToLidMappings, &waHistorySync.PhoneNumberToLIDMapping{PnJID: proto.String(hint.PNJID), LidJID: proto.String(hint.LIDJID)})
		}
		if hint.WAName != "" && hint.PNJID != "" {
			history.Pushnames = append(history.Pushnames, &waHistorySync.Pushname{ID: proto.String(hint.PNJID), Pushname: proto.String(hint.WAName)})
		}
	}
	payload, err := proto.Marshal(history)
	if err != nil {
		return nil, err
	}
	defer clear(payload)
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(compressed.Bytes())
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION.Enum(), HistorySyncNotification: &waE2E.HistorySyncNotification{
		SyncType: waE2E.HistorySyncType_INITIAL_BOOTSTRAP.Enum(), ChunkOrder: proto.Uint32(0), Progress: proto.Uint32(100),
		FileSHA256: hash[:], FileLength: proto.Uint64(uint64(compressed.Len())), InitialHistBootstrapInlinePayload: compressed.Bytes(),
	}}}, nil
}

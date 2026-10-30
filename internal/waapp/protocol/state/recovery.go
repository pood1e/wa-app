package state

import (
	"encoding/json"
	"errors"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

// Export omits registration attempts, device preparation and attestation material.
func (s NativeState) Export() (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	chat, err := curveToProto(s.ChatStatic)
	if err != nil {
		return nil, nil, err
	}
	identity, err := curveToProto(s.Signal.Identity)
	if err != nil {
		return nil, nil, err
	}
	signed, err := preKeyToProto(s.Signal.SignedPreKey)
	if err != nil {
		return nil, nil, err
	}
	credentials := &protocolv1.AccountCredentials{ChatStatic: chat, RegistrationId: s.Signal.RegistrationID, Identity: identity, SignedPreKey: signed}
	if s.Canonical != nil {
		credentials.Canonical = proto.Clone(s.Canonical).(*protocolv1.CanonicalCredentials)
	}
	if s.Signal.PQLastResortPreKey != nil {
		credentials.PqLastResortPreKey = proto.Clone(s.Signal.PQLastResortPreKey).(*protocolv1.SignalPQPreKey)
	}
	for _, key := range s.Signal.PQOneTimePreKeys {
		credentials.PqOneTimePreKeys = append(credentials.PqOneTimePreKeys, proto.Clone(key).(*protocolv1.SignalPQPreKey))
	}
	for _, key := range s.Signal.OneTimePreKeys {
		prekey, err := preKeyToProto(key)
		if err != nil {
			return nil, nil, err
		}
		credentials.OneTimePreKeys = append(credentials.OneTimePreKeys, prekey)
	}

	recovery := &protocolv1.AccountRecoveryState{}
	if err := nativeToProto(s, recovery); err != nil {
		return nil, nil, err
	}
	recovery.Client = s.Client
	if s.AccountReview != nil {
		recovery.AccountReview = proto.Clone(s.AccountReview).(*protocolv1.AccountReviewState)
	}
	if recovery.Client == nil {
		return nil, nil, errors.New("protocol client metadata is required")
	}
	recovery.RemoteIdentities = s.Signal.RemoteIdentities
	recovery.Sessions = make(map[string]*protocolv1.SignalSession, len(s.Signal.Sessions))
	for id, session := range s.Signal.Sessions {
		record := &protocolv1.SignalSession{}
		if err := nativeToProto(session, record); err != nil {
			return nil, nil, err
		}
		recovery.Sessions[id] = record
	}
	return credentials, recovery, nil
}

func Restore(credentials *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) (NativeState, error) {
	if credentials == nil || recovery.GetClient() == nil {
		return NativeState{}, errors.New("account recovery material is incomplete")
	}
	var s NativeState
	if err := protoToNative(recovery, &s); err != nil {
		return NativeState{}, err
	}
	s.Client = proto.Clone(recovery.GetClient()).(*protocolv1.ClientMetadata)
	if recovery.GetAccountReview() != nil {
		s.AccountReview = proto.Clone(recovery.GetAccountReview()).(*protocolv1.AccountReviewState)
	}
	s.ChatStatic = curveFromProto(credentials.GetChatStatic())
	if credentials.GetCanonical() != nil {
		s.Canonical = proto.Clone(credentials.GetCanonical()).(*protocolv1.CanonicalCredentials)
	}
	s.Signal.RegistrationID = credentials.GetRegistrationId()
	s.Signal.Identity = curveFromProto(credentials.GetIdentity())
	s.Signal.SignedPreKey = preKeyFromProto(credentials.GetSignedPreKey())
	if key := credentials.GetPqLastResortPreKey(); key != nil {
		s.Signal.PQLastResortPreKey = proto.Clone(key).(*protocolv1.SignalPQPreKey)
	}
	for _, key := range credentials.GetPqOneTimePreKeys() {
		s.Signal.PQOneTimePreKeys = append(s.Signal.PQOneTimePreKeys, proto.Clone(key).(*protocolv1.SignalPQPreKey))
	}
	for _, key := range credentials.GetOneTimePreKeys() {
		s.Signal.OneTimePreKeys = append(s.Signal.OneTimePreKeys, preKeyFromProto(key))
	}
	s.Signal.RemoteIdentities = recovery.GetRemoteIdentities()
	s.Signal.Sessions = make(map[string]NativeSignalSession, len(recovery.GetSessions()))
	for id, record := range recovery.GetSessions() {
		var session NativeSignalSession
		if err := protoToNative(record, &session); err != nil {
			return NativeState{}, err
		}
		s.Signal.Sessions[id] = session
	}
	s.EnsureMaps()
	return s, nil
}

func curveToProto(key wire.NativeCurveKeyPair) (*protocolv1.CurveKeyPair, error) {
	public, err := wire.DecodeB64Any(key.Public)
	if err != nil {
		return nil, errors.New("invalid protocol public key")
	}
	private, err := wire.DecodeB64Any(key.Private)
	if err != nil {
		return nil, errors.New("invalid protocol private key")
	}
	return &protocolv1.CurveKeyPair{Public: public, Private: private}, nil
}
func curveFromProto(key *protocolv1.CurveKeyPair) wire.NativeCurveKeyPair {
	return wire.NativeCurveKeyPair{Public: wire.B64u(key.GetPublic()), Private: wire.B64u(key.GetPrivate())}
}
func preKeyToProto(key NativeSignalPreKey) (*protocolv1.SignalPreKey, error) {
	pair, err := curveToProto(key.KeyPair)
	if err != nil {
		return nil, err
	}
	return &protocolv1.SignalPreKey{Id: key.ID, KeyPair: pair}, nil
}

func preKeyFromProto(key *protocolv1.SignalPreKey) NativeSignalPreKey {
	return NativeSignalPreKey{ID: key.GetId(), KeyPair: curveFromProto(key.GetKeyPair())}
}

func nativeToProto(value any, dest proto.Message) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, dest)
}
func protoToNative(value proto.Message, dest any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func AndroidClientMetadata(s NativeState, version string) *protocolv1.ClientMetadata {
	p := NormalizeNativePhoneProfile(s.Profile)
	return &protocolv1.ClientMetadata{
		Platform: protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, AppVersion: NativeAppVersion(version),
		OsVersion: p.AndroidVersion, Manufacturer: p.DeviceVendor, Device: p.DeviceModel, OsBuildNumber: p.BuildDisplayID,
		PhoneId: p.FDID, LocaleLanguage: "en", LocaleCountry: "US", DeviceExperimentId: p.ExpIDUUID,
		ModelType: "phone", Mcc: p.AdditionalMapFields["mcc"], Mnc: p.AdditionalMapFields["mnc"],
	}
}

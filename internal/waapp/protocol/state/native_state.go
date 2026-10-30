package state

import (
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const NativeStateSchema = "byte-v-forge-wa-app-native-state/v1"

type NativeState struct {
	Canonical            *protocolv1.CanonicalCredentials          `json:"-"`
	Client               *protocolv1.ClientMetadata                `json:"client,omitempty"`
	AccountReview        *protocolv1.AccountReviewState            `json:"-"`
	Schema               string                                    `json:"schema"`
	CreatedAtUnix        int64                                     `json:"created_at_unix"`
	CC                   string                                    `json:"cc"`
	Phone                string                                    `json:"phone"`
	AuthKey              string                                    `json:"authkey"`
	PushName             string                                    `json:"push_name,omitempty"`
	Profile              NativePhoneProfile                        `json:"profile"`
	KeyBundle            NativeKeyBundle                           `json:"key_bundle"`
	GenerateCodeAttempts int                                       `json:"reg_attempts_generate_code,omitempty"`
	LastCodeParams       map[string]string                         `json:"last_code_params,omitempty"`
	LastCodeResult       map[string]any                            `json:"last_code_result,omitempty"`
	LastRegister         map[string]any                            `json:"last_register,omitempty"`
	PreChatdAB           NativePreChatdABState                     `json:"pre_chatd_ab,omitempty"`
	RegistrationJID      string                                    `json:"registration_jid,omitempty"`
	ChatRoutingInfo      string                                    `json:"chat_routing_info,omitempty"`
	ChatConnection       *protocolv1.ChatConnectionState           `json:"chat_connection,omitempty"`
	ChatStatic           pwire.NativeCurveKeyPair                  `json:"chat_static"`
	Signal               NativeSignalState                         `json:"signal"`
	AppState             NativeAppState                            `json:"app_state,omitempty"`
	ContactHints         []wacore.WAContactHint                    `json:"contact_hints,omitempty"`
	MessagePayloads      map[string]NativeMessagePayload           `json:"message_payloads,omitempty"`
	MessagePlainRef      map[string]string                         `json:"message_plain_ref,omitempty"`
	PrivacyTokens        map[string]NativePrivacyToken             `json:"privacy_tokens,omitempty"`
	SenderKeyRecords     map[string][]byte                         `json:"sender_key_records,omitempty"`
	SentMessages         map[string]*protocolv1.SentMessage        `json:"sent_messages,omitempty"`
	SignalRetries        map[string]*protocolv1.SignalRetryState   `json:"signal_retries,omitempty"`
	PendingSignalRetries map[string]*protocolv1.PendingSignalRetry `json:"pending_signal_retries,omitempty"`
}

type NativePhoneProfile struct {
	Schema              string            `json:"schema"`
	CreatedAtUnix       int64             `json:"created_at_unix"`
	PhoneSHA256         string            `json:"phone_sha256"`
	DeviceVendor        string            `json:"device_vendor"`
	DeviceModel         string            `json:"device_model"`
	AndroidVersion      string            `json:"android_version"`
	BuildDisplayID      string            `json:"build_display_id,omitempty"`
	FDID                string            `json:"fdid"`
	ExpID               string            `json:"expid"`
	ExpIDUUID           string            `json:"expid_uuid"`
	AccessSessionID     string            `json:"access_session_id"`
	AccessSessionIDUUID string            `json:"access_session_id_uuid"`
	ID                  string            `json:"id"`
	IDHex               string            `json:"id_hex"`
	BackupToken         string            `json:"backup_token"`
	BackupTokenHex      string            `json:"backup_token_hex"`
	AdvertisingID       string            `json:"advertising_id,omitempty"`
	AdditionalMapFields map[string]string `json:"additional_map_fields"`
}

type NativeKeyBundle struct {
	RegistrationID int32  `json:"registration_id"`
	SignedPreKeyID int32  `json:"signed_prekey_id"`
	IdentityPublic string `json:"e_ident"`
	KeyType        string `json:"e_keytype"`
	RegID          string `json:"e_regid"`
	SignedKeyID    string `json:"e_skey_id"`
	SignedKeyValue string `json:"e_skey_val"`
	SignedKeySig   string `json:"e_skey_sig"`
}

type NativeSignalPreKey struct {
	ID      int32                    `json:"id"`
	KeyPair pwire.NativeCurveKeyPair `json:"key_pair"`
}

type NativeSignalState struct {
	RegistrationID     int32                          `json:"registration_id"`
	Identity           pwire.NativeCurveKeyPair       `json:"identity"`
	SignedPreKey       NativeSignalPreKey             `json:"signed_prekey"`
	OneTimePreKeys     []NativeSignalPreKey           `json:"one_time_prekeys,omitempty"`
	RemoteIdentities   map[string]string              `json:"remote_identities,omitempty"`
	Sessions           map[string]NativeSignalSession `json:"sessions,omitempty"`
	PQLastResortPreKey *protocolv1.SignalPQPreKey     `json:"pq_last_resort_pre_key,omitempty"`
	PQOneTimePreKeys   []*protocolv1.SignalPQPreKey   `json:"pq_one_time_pre_keys,omitempty"`
}

type NativeSignalSession struct {
	Sender               string                          `json:"sender"`
	Version              int                             `json:"version"`
	RemoteIdentityPublic string                          `json:"remote_identity_public"`
	RootKey              string                          `json:"root_key,omitempty"`
	SenderRatchetPublic  string                          `json:"sender_ratchet_public,omitempty"`
	SenderRatchetPrivate string                          `json:"sender_ratchet_private,omitempty"`
	SenderChain          *NativeSenderChain              `json:"sender_chain,omitempty"`
	ReceiverChains       map[string]NativeReceiverChain  `json:"receiver_chains,omitempty"`
	PreviousCounter      *int                            `json:"previous_counter,omitempty"`
	RemoteRegistrationID *int                            `json:"remote_registration_id,omitempty"`
	AliceBaseKey         string                          `json:"alice_base_key,omitempty"`
	PendingPreKey        *protocolv1.SignalPendingPreKey `json:"pending_pre_key,omitempty"`
}

type NativeAppState struct {
	Keys        map[string]NativeAppStateKey        `json:"keys,omitempty"`
	Collections map[string]NativeAppStateCollection `json:"collections,omitempty"`
}

type NativeAppStateKey struct {
	KeyID       string `json:"key_id"`
	KeyData     string `json:"key_data"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
}

type NativeAppStateCollection struct {
	Version        uint64            `json:"version,omitempty"`
	Hash           string            `json:"hash,omitempty"`
	IndexValueMACs map[string]string `json:"index_value_macs,omitempty"`
}

type NativeReceiverChain struct {
	SkippedMessageKeys map[uint32]*protocolv1.SignalMessageKeys `json:"skipped_message_keys,omitempty"`
	RatchetKey         string                                   `json:"ratchet_key"`
	ChainKey           string                                   `json:"chain_key"`
	Index              int                                      `json:"index"`
	RootKey            string                                   `json:"root_key,omitempty"`
}

type NativeSenderChain struct {
	RatchetKey string `json:"ratchet_key"`
	ChainKey   string `json:"chain_key"`
	Index      int    `json:"index"`
}

type NativeMessagePayload struct {
	Contact             string                 `json:"contact,omitempty"`
	Sender              string                 `json:"sender,omitempty"`
	ContactPN           string                 `json:"contact_pn,omitempty"`
	SenderPN            string                 `json:"sender_pn,omitempty"`
	NotifyName          string                 `json:"notify_name,omitempty"`
	ParticipantUsername string                 `json:"participant_username,omitempty"`
	ContactHints        []wacore.WAContactHint `json:"contact_hints,omitempty"`
	EncType             string                 `json:"enc_type,omitempty"`
	Path                string                 `json:"path,omitempty"`
	Payload             string                 `json:"payload"`
	ProviderMessageID   string                 `json:"provider_message_id,omitempty"`
	ProviderTimestamp   int64                  `json:"provider_timestamp,omitempty"`
	RetryCount          uint32                 `json:"retry_count,omitempty"`
}

type NativePrivacyToken struct {
	Token     string `json:"token"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

func (s *NativeState) EnsureMaps() {
	if s.SentMessages == nil {
		s.SentMessages = make(map[string]*protocolv1.SentMessage)
	}
	if s.SignalRetries == nil {
		s.SignalRetries = make(map[string]*protocolv1.SignalRetryState)
	}
	if s.PendingSignalRetries == nil {
		s.PendingSignalRetries = make(map[string]*protocolv1.PendingSignalRetry)
	}
	if s.MessagePayloads == nil {
		s.MessagePayloads = map[string]NativeMessagePayload{}
	}
	if s.MessagePlainRef == nil {
		s.MessagePlainRef = map[string]string{}
	}
	if s.PrivacyTokens == nil {
		s.PrivacyTokens = map[string]NativePrivacyToken{}
	}
	if s.Signal.RemoteIdentities == nil {
		s.Signal.RemoteIdentities = map[string]string{}
	}
	if s.Signal.Sessions == nil {
		s.Signal.Sessions = map[string]NativeSignalSession{}
	}
	if s.AppState.Keys == nil {
		s.AppState.Keys = map[string]NativeAppStateKey{}
	}
	if s.AppState.Collections == nil {
		s.AppState.Collections = map[string]NativeAppStateCollection{}
	}
}

func BuildNativeOneTimePreKeys(count int) []NativeSignalPreKey {
	out := make([]NativeSignalPreKey, 0, count)
	for i := 0; i < count; i++ {
		kp, err := pwire.NewNativeCurveKeyPair()
		if err != nil {
			break
		}
		out = append(out, NativeSignalPreKey{ID: int32(i + 1), KeyPair: kp})
	}
	return out
}

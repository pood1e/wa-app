package communication

import (
	"context"
	"strconv"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *connectionState) authorizeDevice(ctx context.Context, qr *wire.DevicePairingQR, list *waAdv.ADVKeyIndexList, indexes []uint32, client *security.Client) (wire.ChatdNode, bool, error) {
	for attempt := 0; ; attempt++ {
		metadata, err := client.PairingMetadata(ctx, s.current.Canonical)
		if err != nil {
			return wire.ChatdNode{}, false, err
		}
		response, uncertain, err := s.submitDevicePairing(ctx, qr, list, indexes, metadata)
		if err != nil {
			return response, uncertain, err
		}
		if _, ok := ChatdChild(response, "device"); ok {
			return response, false, nil
		}
		for _, tag := range []string{"retry-after-challenges", "retry-with-method"} {
			if _, exists := ChatdChild(response, tag); exists {
				return response, false, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "device linking requires native verification: "+tag, false)
			}
		}
		retry, exists := ChatdChild(response, "retry-ts")
		if !exists {
			return response, false, nil
		}
		timestamp, err := strconv.ParseUint(retry.Attrs["ts"], 10, 64)
		if err != nil || timestamp == 0 {
			return response, false, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "device linking timestamp correction is malformed", false)
		}
		if timestamp > uint64(max(0, s.connection.clock.Now().Unix())+86400) {
			return response, false, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "device linking timestamp correction exceeds the native time window", false)
		}
		if attempt != 0 {
			return response, false, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "device linking timestamp correction was repeated", false)
		}
		// The native continuation re-signs the same reserved index once with server time.
		list.Timestamp = proto.Uint64(timestamp)
	}
}

func (s *connectionState) submitDevicePairing(ctx context.Context, qr *wire.DevicePairingQR, list *waAdv.ADVKeyIndexList, indexes []uint32, metadata wire.ChatdNode) (wire.ChatdNode, bool, error) {
	c := s.connection
	credentials, _, err := s.current.Export()
	if err != nil {
		return wire.ChatdNode{}, false, err
	}
	proof, err := wire.SignDevicePairing(credentials.GetIdentity(), &waAdv.ADVDeviceIdentity{RawID: list.RawID, Timestamp: list.Timestamp,
		KeyIndex: list.CurrentIndex, AccountType: waAdv.ADVEncryptionType_E2EE.Enum(), DeviceType: waAdv.ADVEncryptionType_E2EE.Enum()}, qr, indexes)
	if err != nil {
		return wire.ChatdNode{}, false, err
	}
	defer clear(proof.NonceKey)
	clientProps, err := proto.Marshal(&waCompanionReg.ClientPairingProps{IsChatDbLidMigrated: proto.Bool(true)})
	if err != nil {
		return wire.ChatdNode{}, false, err
	}
	if s.current.LinkedDevices == nil {
		s.current.LinkedDevices = new(protocolv1.LinkedDeviceState)
	}
	s.current.LinkedDevices.SignedKeyIndexList = proof.KeyIndexList
	// Persist the reservation and each authorized timestamp correction before sending.
	if err := c.SaveState(ctx, c.Input.ClientProfileID, s.current); err != nil {
		return wire.ChatdNode{}, false, err
	}
	response, err := s.query(ctx, linkedDeviceIQ(c.ids.NewID("wa_link_"), "pair-device", nil, []wire.ChatdNode{
		{Tag: "ref", Content: qr.Reference}, {Tag: "pub-key", Content: qr.NoisePublic},
		{Tag: "device-identity", Content: proof.Identity},
		{Tag: "key-index-list", Attrs: map[string]string{"ts": strconv.FormatUint(list.GetTimestamp(), 10)}, Content: proof.KeyIndexList},
		{Tag: "client-props", Content: clientProps},
		{Tag: "pem", Attrs: map[string]string{"version": "1", "algorithm": "rsa2048"}, Content: []wire.ChatdNode{
			{Tag: "pem", Content: proof.NonceKey}, {Tag: "ttl", Attrs: map[string]string{"ts_s": "432000"}}, {Tag: "key_id", Content: "1"},
		}}, metadata,
	}))
	return response, err != nil && response.Tag == "", err
}

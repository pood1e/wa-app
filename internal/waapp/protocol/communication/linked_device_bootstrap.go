package communication

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) flushLinkedDeviceBootstrap(ctx context.Context, current *state.NativeState) error {
	for _, device := range current.LinkedDevices.GetDevices() {
		if device.GetBootstrapSent() || c.clock.Now().Unix() < device.GetBootstrapRetryAt() {
			continue
		}
		device.BootstrapRetryAt = c.clock.Now().Add(30 * time.Second).Unix()
		if err := c.SaveState(ctx, c.Input.ClientProfileID, *current); err != nil {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := c.bootstrapLinkedDevice(attempt, current, device)
		cancel()
		if err != nil {
			log.Printf("WA companion initialization deferred code=%s: %v", shared.ToProtoError(err).GetCode(), shared.SanitizeLogError(err))
			return nil
		}
		device.BootstrapSent = true
		device.BootstrapRetryAt = 0
		return c.SaveState(ctx, c.Input.ClientProfileID, *current)
	}
	return nil
}

func (c *Connection) bootstrapLinkedDevice(ctx context.Context, current *state.NativeState, device *protocolv1.LinkedDeviceRecord) error {
	if _, err := ownLinkedDevice(current, device.GetDeviceRef()); err != nil {
		return err
	}
	if err := prepareLinkedDeviceKeys(current, c.clock.Now()); err != nil {
		return err
	}
	if err := c.SaveState(ctx, c.Input.ClientProfileID, *current); err != nil {
		return err
	}
	share, err := linkedDeviceKeyShare(current)
	if err != nil {
		return err
	}
	if err := c.sendLinkedDeviceControl(ctx, current, device, "keys", share); err != nil {
		return err
	}
	if current.AppState.Collections[WaAppStatePushNameCollection].Version == 0 {
		if err := c.publishLinkedDeviceProfile(ctx, current); err != nil {
			return err
		}
	}
	metadata, err := linkedDeviceInitialMetadata(current)
	if err != nil {
		return err
	}
	return c.sendLinkedDeviceControl(ctx, current, device, "metadata", metadata)
}

func (c *Connection) sendLinkedDeviceControl(ctx context.Context, current *state.NativeState, device *protocolv1.LinkedDeviceRecord, step string, message *waE2E.Message) error {
	id := "device_bootstrap_" + strconv.FormatUint(uint64(device.GetKeyIndex()), 10) + "_" + strconv.FormatInt(device.GetLinkedAt(), 10) + "_" + step
	if previous := findSentMessage(current, device.GetDeviceRef(), id); previous != nil {
		message = new(waE2E.Message)
		if err := proto.Unmarshal(previous.GetMessage(), message); err != nil {
			return err
		}
	}
	input := wacore.EngineMessageSendInput{WAAccountID: c.Input.WAAccountID, ClientProfileID: c.Input.ClientProfileID, RegisteredIdentityID: c.Input.RegisteredIdentityID,
		AppVersion: c.Input.AppVersion, ContactJID: device.GetDeviceRef(), ClientMessageID: id, Message: message}
	result := c.Engine.SendMessageOnSession(ctx, c.Session, current, input, c.Input, ContextBoundTimeout(ctx, 10*time.Second))
	c.BufferPendingLocked(result.Items, result.Update)
	return errors.Join(result.Err, c.ApplyMessageSendUpdate(ctx, c.Input.ClientProfileID, current, c.Input, result.Items, result.Update))
}

func (c *Connection) publishLinkedDeviceProfile(ctx context.Context, current *state.NativeState) error {
	request, collection, err := BuildNativePushNamePatch(current, current.PushName, uint64(max(0, c.clock.Now().UnixMilli())))
	if err != nil {
		return err
	}
	request.Attrs["id"] = c.ids.NewID("wa_bootstrap_profile_")
	response, update, err := c.sendIQLocked(ctx, *current, c.Input.RegisteredIdentityID, c.Input.AppVersion, request, "companion profile initialization timed out")
	ApplyChatdSessionUpdateState(current, update)
	if err != nil {
		return err
	}
	if err := ChatdIQError(response); err != nil {
		return err
	}
	if err := linkedDeviceCollectionError(response); err != nil {
		return err
	}
	current.AppState.Collections[WaAppStatePushNameCollection] = collection
	return c.SaveState(ctx, c.Input.ClientProfileID, *current)
}

func linkedDeviceCollectionError(node wire.ChatdNode) error {
	if node.Tag == "error" {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "companion app-state collection was rejected", false)
	}
	for _, child := range wire.ChatdChildren(node) {
		if err := linkedDeviceCollectionError(child); err != nil {
			return err
		}
	}
	return nil
}

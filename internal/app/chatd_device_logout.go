package app

import (
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// chatdDeviceLogout 表示服务端经长连接下发的 device_logout 通知:该号码已在其他
// 设备上注册(账号被转移/接管)或被远程登出,本端登录态随之作废。对齐 APK
// RegistrationNotificationHandler 中与 wa_old_registration 并排的 device_logout 分支。
type chatdDeviceLogout struct {
	id                  string
	device              string
	newDevicePlatform   string
	newDeviceAppVersion string
}

func deviceLogoutFromChatdNode(node chatdNode) (chatdDeviceLogout, bool) {
	if node.Tag != "notification" {
		return chatdDeviceLogout{}, false
	}
	child, ok := chatdChild(node, "device_logout")
	if !ok {
		return chatdDeviceLogout{}, false
	}
	id := strings.TrimSpace(child.Attrs["id"])
	if id == "" {
		return chatdDeviceLogout{}, false
	}
	return chatdDeviceLogout{
		id:                  id,
		device:              strings.TrimSpace(child.Attrs["device"]),
		newDevicePlatform:   strings.TrimSpace(child.Attrs["new_device_platform"]),
		newDeviceAppVersion: strings.TrimSpace(child.Attrs["new_device_app_version"]),
	}, true
}

// accountLogoutFromUpdate 把入站解析出的 device_logout 提升为引擎层登出事件,供
// receiveMessageBatch 据此作废登录态、停连。
func accountLogoutFromUpdate(l *chatdDeviceLogout) *EngineAccountLogout {
	if l == nil {
		return nil
	}
	return &EngineAccountLogout{
		Reason:              accountLoggedOutMessage(l),
		NewDevicePlatform:   l.newDevicePlatform,
		NewDeviceAppVersion: l.newDeviceAppVersion,
	}
}

func accountLoggedOutError(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "account registered on another device"
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, reason, false)
}

// chatdDeviceRemovedMarker 是 device_removed 登出信号在错误消息里的稳定标记,贯穿
// chatd 错误构造与 long-connection 的 isDeviceRemovedError 识别。
const chatdDeviceRemovedMarker = "device_removed"

// chatdTerminalNodeDeviceRemoved 判断 chatd 终端控制节点(stream:error/failure/error)是否携带
// <conflict type="device_removed">:本设备已被服务端移除(号码已在其他设备注册/被接管)。对齐 APK
// X.1FJ(ErrorStanzaHandler):取 <conflict> 子节点、读 type 属性、等于 "device_removed" 即触发登出。
func chatdTerminalNodeDeviceRemoved(node chatdNode) bool {
	if chatdNodeIsDeviceRemovedConflict(node) {
		return true
	}
	if children, ok := node.Content.([]chatdNode); ok {
		for _, child := range children {
			if chatdNodeIsDeviceRemovedConflict(child) {
				return true
			}
		}
	}
	return false
}

func chatdNodeIsDeviceRemovedConflict(node chatdNode) bool {
	return node.Tag == "conflict" && strings.TrimSpace(node.Attrs["type"]) == chatdDeviceRemovedMarker
}

// deviceRemovedError 构造 device_removed 登出错误:非可重试 CONFLICT,消息以 device_removed 标记开头,
// 供 chatdReceiveError 透传后由 isDeviceRemovedError 识别。
func deviceRemovedError(summary string) error {
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, chatdDeviceRemovedMarker+": "+summary, false)
}

func accountLoggedOutMessage(l *chatdDeviceLogout) string {
	if l != nil && l.newDevicePlatform != "" {
		return "account registered on another device (" + l.newDevicePlatform + ")"
	}
	return "account registered on another device"
}

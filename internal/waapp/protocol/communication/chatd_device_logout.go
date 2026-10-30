package communication

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// chatdDeviceLogout 表示服务端经长连接下发的 device_logout 通知:该号码已在其他
// 设备上注册(账号被转移/接管)或被远程登出,本端登录态随之作废。对齐 APK
// RegistrationNotificationHandler 中与 wa_old_registration 并排的 device_logout 分支。
type ChatdDeviceLogout struct {
	Id                  string
	Device              string
	NewDevicePlatform   string
	NewDeviceAppVersion string
}

func DeviceLogoutFromChatdNode(node pwire.ChatdNode) (ChatdDeviceLogout, bool) {
	if node.Tag != "notification" {
		return ChatdDeviceLogout{}, false
	}
	child, ok := ChatdChild(node, "device_logout")
	if !ok {
		return ChatdDeviceLogout{}, false
	}
	id := strings.TrimSpace(child.Attrs["id"])
	if id == "" {
		return ChatdDeviceLogout{}, false
	}
	return ChatdDeviceLogout{
		Id:                  id,
		Device:              strings.TrimSpace(child.Attrs["device"]),
		NewDevicePlatform:   strings.TrimSpace(child.Attrs["new_device_platform"]),
		NewDeviceAppVersion: strings.TrimSpace(child.Attrs["new_device_app_version"]),
	}, true
}

// accountLogoutFromUpdate 把入站解析出的 device_logout 提升为引擎层登出事件,供
// receiveMessageBatch 据此作废登录态、停连。
func AccountLogoutFromUpdate(l *ChatdDeviceLogout) *wacore.EngineAccountLogout {
	if l == nil {
		return nil
	}
	return &wacore.EngineAccountLogout{
		Reason:              AccountLoggedOutMessage(l),
		NewDevicePlatform:   l.NewDevicePlatform,
		NewDeviceAppVersion: l.NewDeviceAppVersion,
	}
}

func AccountLoggedOutError(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "account registered on another device"
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, reason, false)
}

// chatdAccountTakeoverMarker 是"账号被接管"登出信号在错误消息里的稳定标记,贯穿 chatd 错误构造
// 与 long-connection 的 isAccountTakeoverError 识别。
const ChatdAccountTakeoverMarker = "account_takeover"

// chatdAccountTakeoverConflictTypes 是 chatd <conflict type=…> 中表示"本设备已被接管/登出"的取值。
// 只收 device_removed:设备被服务端移除(号码已在其他设备注册),对齐 APK X.1FJ ErrorStanzaHandler
// 唯一触发 deregister 的判定。
//
// 刻意【不含】replaced:官方对 <conflict type="replaced"> 只重连、从不登出。replaced(会话被顶替)
// 本质不是可靠的转出信号——即便把 usync 收敛到同一条长连接(每账号一条 chatd)消除了稳态自我并发,
// 部署滚动(新旧 pod 长连接重叠)与重连竞态仍会让服务端对同一身份回 replaced。实测:重新启用 replaced
// 判转出后,一个健康在线号在新 pod 首次连接(count=0)即被 replaced 误判转出。故 replaced 一律按重连处理,
// 真正的转出/登出只认 device_removed 与 device_logout。
var ChatdAccountTakeoverConflictTypes = map[string]struct{}{
	"device_removed": {},
}

// chatdTerminalNodeAccountTakeover 判断 chatd 终端控制节点(stream:error/failure/error)是否携带
// 表示账号被接管的 <conflict type="device_removed">(号码已在其他设备注册)。
func ChatdTerminalNodeAccountTakeover(node pwire.ChatdNode) bool {
	if ChatdConflictIsAccountTakeover(node) {
		return true
	}
	if children, ok := node.Content.([]pwire.ChatdNode); ok {
		for _, child := range children {
			if ChatdConflictIsAccountTakeover(child) {
				return true
			}
		}
	}
	return false
}

func ChatdConflictIsAccountTakeover(node pwire.ChatdNode) bool {
	if node.Tag != "conflict" {
		return false
	}
	_, ok := ChatdAccountTakeoverConflictTypes[strings.TrimSpace(node.Attrs["type"])]
	return ok
}

// accountTakenOverError 构造账号被接管的登出错误:非可重试 CONFLICT,消息以 account_takeover 标记开头,
// 供 chatdReceiveError 透传后由 isAccountTakeoverError 识别。
func AccountTakenOverError(summary string) error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, ChatdAccountTakeoverMarker+": "+summary, false)
}

func AccountLoggedOutMessage(l *ChatdDeviceLogout) string {
	if l != nil && l.NewDevicePlatform != "" {
		return "account registered on another device (" + l.NewDevicePlatform + ")"
	}
	return "account registered on another device"
}

// isAccountTakeoverError 判断错误是否为 chatd 下发的"账号被接管"登出信号:服务端在
// <stream:error>/<failure> 里带 <conflict type="device_removed"|"replaced">,表示本设备登录态已失效
// (号码已在其他设备注册)。对齐 APK ErrorStanzaHandler(X.1FJ)对 conflict type 的登出判定。
// 经 chatdReceiveError 保留为非可重试 CONFLICT,消息含 account_takeover 标记,区别于 generic failure。
func IsAccountTakeoverError(err error) bool {
	if err == nil {
		return false
	}
	protoErr := shared.ToProtoError(err)
	return protoErr.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT &&
		strings.Contains(protoErr.GetMessage(), ChatdAccountTakeoverMarker)
}

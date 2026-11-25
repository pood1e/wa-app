# 关联设备

在账号详情打开“关联设备”，上传 WhatsApp Web 二维码截图或粘贴二维码内容，再点击授权。截图通过浏览器本地 ZXing WASM 解码；授权提交使用敏感请求体。关联设备获得该账号的消息访问权限，退出操作由用户确认。

“清空二维码”重置截图选择和二维码输入，提交授权时也立即清空表单，完成后释放本次请求缓存。注册与邮箱验证码提供“清空验证码”，提交或重新发码时清空对应输入。已关联设备通过“退出设备”撤销授权。

## 连接与状态

1. 复用主设备现有连接，通过 PN / LID 查询设备与签名索引，验证身份并保留活动设备索引。
2. 解析四段 / 五段二维码及官方深链，使用官方 `WAAdv` Proto、Signal 签名、二维码 HMAC 和派生 nonce 构造授权材料。
3. 通过现有原生安全流程恢复 Canonical 凭证，按服务端证书双层加密，随 `md / pair-device` 提交。Canonical 材料与用户两步验证密码分别保存。
4. 发送前保存索引预留；服务端 `retry-ts` 在原生时间窗口内校正一次。授权确认后保存设备资料及 Signal 身份。
5. 接收循环通过加密 peer 消息发送 AppState 密钥、账户名称设置和压缩初始化数据。稳定消息 ID、棘轮与检查点支持重启续传，初始化覆盖账户设置、现有联系人名称及 PN / LID 映射。
6. 退出后发布剩余设备的签名索引，清理该设备的资料、会话及初始化发件箱。设备发现采用较新的有效索引过滤过期条目。

签名索引和设备进度保存在 `AccountRecoveryState.linked_devices`，随所选 SQL 后端恢复。二维码与派生 nonce 属于本次授权的临时材料。相同二维码的提交结果按账号缓存五分钟，`outcome_unknown` 提示检查 Web 页面及设备列表。

## 接口与代码

| HTTP | RPC |
| --- | --- |
| `GET /api/wa/linked-devices?wa_account_id=...` | `ListLinkedDevices` |
| `POST /api/wa/linked-devices` | `LinkDevice` |
| `POST /api/wa/linked-devices/unlink` | `UnlinkDevice` |

- 契约：`proto/byte/v/forge/waapp/{v1,protocol/v1}/linked_devices.proto`。
- QR 与签名：`internal/waapp/protocol/wire/device_pairing_*.go`。
- 连接与初始化：`internal/waapp/protocol/communication/linked_device*.go`、`peer_message.go`。
- Canonical 证明：`internal/waapp/protocol/security/device_pairing.go`。
- 编排与页面：`internal/waapp/app/account/linked_devices.go`、`webui/src/dashboard/wa-linked-devices*`。

## 验证范围

[对等实现的验证记录](https://github.com/pood1e/wa-app-v2/blob/3c30e91a25fb9f8d5d839abce2fa449448375009/docs/architecture/linked-devices.md)覆盖 Android / iOS 凭证进入官方 Web 聊天页、刷新保活、撤销退出，以及 iOS 重新关联。另一个 Android 账号的新关联收到重复 `retry-ts`，返回 `REJECTED`；其主设备认证单独通过。

本仓库回归覆盖加密连接、索引校验、初始化恢复、退出清理、SQLite 持久化，以及桌面 / 移动页面的显式授权与未知结果。完整历史消息回填列入后续验收范围。

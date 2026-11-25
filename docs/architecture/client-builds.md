# 客户端协议基线

本轮同步基线为 2026-11-23 构建材料及 2026-11-25 关联设备协议。新添加流程使用随包版本，已有账号沿用保存的客户端元数据、凭证和恢复状态。

| 平台 | 账号类型 | 版本 / build |
| --- | --- | --- |
| Android | 个人版 | `2.26.46.71 / 264607149` |
| Android | 商业版 | `2.26.45.83 / 264508303` |
| iOS | 个人版 | `26.46.72 / 1120481811` |
| iOS | 商业版 | `26.46.72 / 1120481847` |

Android 构建材料绑定同版 APK 的证书、DEX、资源和权限摘要。iOS 材料按应用身份校验版本与构建 hash，个人版和商业版分别使用对应 token 常量与平台标识。

## 注册与通信

Android `next_method_required` / `sms_required` 按原生通道顺序续接，保留原协议实例、账号密钥及流程来源。页面遵守冷却，由用户显式请求指定通道的新验证码；续接结果通过现有 Proto 枚举表达。

原生通信复用 whatsmeow 的官方 Proto、节点编解码和 JID 类型，Signal 使用 libsignal `0.2.3`。配对签名、Canonical 授权、AppState 初始化与撤销索引发布由主设备模块编排。

| 代码 | 职责 |
| --- | --- |
| `internal/waapp/protocol/onboarding/android_build.go`、`android_business.go` | Android 构建材料 |
| `internal/waapp/protocol/onboarding/ios/profile.go` | iOS 平台材料与校验 |
| `internal/waapp/protocol/onboarding/registration_continuation.go` | 必选验证动作 |
| `internal/waapp/app/onboarding/verification_continuation.go` | 流程续接与冷却 |
| `webui/src/dashboard/wa-account-add-state.ts` | 页面验证状态 |

## 验证范围

[对等实现的构建记录](https://github.com/pood1e/wa-app-v2/blob/3c30e91a25fb9f8d5d839abce2fa449448375009/docs/architecture/client-builds.md)保留安装包摘要、原生定位与分版本验收。新版 Android 个人版、iOS 个人版和 iOS 商业版通过保存凭证的被动 chatd 认证；完整注册、转移和收发按各次实测版本记录。

本仓库同步相同协议实现，使用 `scripts/lint-go.sh`、`go test -race ./cmd/... ./internal/...` 及前端 test / lint / build 验证。真实后端集成由 `WA_TEST_PG_DSN`、`WA_TEST_VALKEY_URL` 指定，CI 提供独立 PostgreSQL 与 Valkey 实例。

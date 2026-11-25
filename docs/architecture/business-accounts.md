# WhatsApp Business 账号

账号详情的“转为商业账号”选择 Android / iOS 平台和商家名称，通过同一号码验证、商业版认证与名称回读后，接纳回原账号记录。联系人、聊天历史和旧身份的解密材料继续保留。

## 协议与恢复

- Android 使用商业版应用身份、`SMBA` UA 和对应安装包材料。注册、密码续接复用同一商家名称证书及密钥。
- iOS 使用 `net.whatsapp.WhatsAppSMB`、`SMB` UA 和独立构建常量，注册时签署空名称证书，登录后设置目标商家名称。
- 名称证书复用官方 `waVnameCert` Proto，由当前 Signal 身份签名；`w:biz` 更新后回读名称、证书与身份归属。
- 账号类型由 `model/v1/account_type.proto` 统一定义，类型、证书与客户端元数据随账号持久化，重启后恢复对应商业版连接。

构建版本见[客户端基线](client-builds.md)。已有 PostgreSQL 部署执行一次[字段升级](../migration.md#商业账号字段升级)，新建数据库采用当前初始化 schema；SQLite 沿用 Proto 记录。

## 代码与验证

| 代码 | 职责 |
| --- | --- |
| `internal/waapp/protocol/onboarding/android_business.go` | Android 商业版构建与注册 |
| `internal/waapp/protocol/onboarding/ios/profile.go`、`parameters.go` | iOS 商业版材料与注册 |
| `internal/waapp/protocol/wire/business_certificate.go` | 名称证书签名 |
| `internal/waapp/protocol/communication/business_account.go` | 名称更新与回读 |
| `internal/waapp/app/account/acceptance_transaction.go` | 同号码账号接纳 |
| `webui/src/dashboard/wa-account-type.tsx` | 转换入口与账号类型展示 |

[对等实现的验证记录](https://github.com/pood1e/wa-app-v2/blob/3c30e91a25fb9f8d5d839abce2fa449448375009/docs/architecture/business-accounts.md)覆盖 iOS 个人版转入 Android 商业版，以及 Android 商业版转入 iOS 商业版的认证、接纳和重启恢复。本仓库同步对应实现，并保留证书归属、接纳恢复及双平台页面回归。

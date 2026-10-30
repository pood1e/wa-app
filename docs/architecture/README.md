# wa-app 架构

## 服务与依赖

~~~mermaid
flowchart LR
    UI[Dashboard] --> HTTP[HTTP API]
    HTTP --> Account[账号运行应用]
    HTTP --> AddRPC[添加服务 gRPC]
    AddRPC --> Onboarding[账号添加应用]
    Onboarding --> Protocol[协议适配]
    Onboarding --> AcceptRPC[运行服务接纳 gRPC]
    AcceptRPC --> Account
    Protocol --> Worker[可选设备 SDK worker]
    Account --> Protocol
    Account --> SQL[(PostgreSQL / SQLite)]
    Account --> Runtime[内存 / Valkey]
~~~

两个进程由各自组合根装配。运行服务持有持久数据和账号连接，添加服务持有短期流程，通过内部 gRPC 交付账号材料。

| 代码 | 职责 |
| --- | --- |
| cmd/wa-app-service | 账号运行服务组合根 |
| cmd/wa-account-add-service | 账号添加服务组合根 |
| internal/waapp/app/account | 账号、消息、联系人、群管理、设置和复核用例 |
| internal/waapp/app/onboarding | 注册、转移、密码验证、取消、超时和交付 |
| internal/waapp/httpapi | 同源 Dashboard、鉴权和 Proto JSON |
| internal/waapp/rpc | 生成契约适配、服务认证和标准 health |
| internal/waapp/session | 连接恢复、启停、所有权租约和账号连接交接 |
| internal/waapp/protocol | 注册、通信、安全、传输及协议状态适配 |
| internal/waapp/store | PostgreSQL / SQLite 持久化与事务 |
| internal/waapp/runtime | 内存 / Valkey 缓存和租约 |
| webui/src/dashboard | 账号管理与聊天界面 |

应用层定义消费方端口，基础设施实现这些端口，组合根完成注入。Proto 是领域模型、RPC、状态和错误语义的唯一真源，Go 与前端类型由脚本生成。

## 数据生命周期

| 数据 | 存储与清理 |
| --- | --- |
| 添加流程、验证输入和待交付材料 | 添加服务内存；完成、取消或过期时清理 |
| 账号、联系人、消息、代理配置和接纳回执 | 运行服务 SQL |
| AccountCredentials / AccountRecoveryState | 私有 Proto，运行服务 SQL |
| 临时缓存、会话存活和 holder 租约 | 默认内存 TTL，可选 Valkey TTL |
| 设备 SDK 流程上下文 | 独立 worker 内存；释放或过期时清理 |

公开接口通过 SensitiveText 表达敏感值，按请求权限提供正文或脱敏值。两服务共享服务认证，worker 使用私有 socket 或 SSH 转发。

## 流程与连接

1. 添加服务完成验证后，导出账号材料并关闭临时连接，再向运行服务交付。
2. 运行服务在同一 SQL 事务保存账号、凭证、恢复状态和接纳回执，提交后激活会话；重复交付凭请求标识查询原结果。
3. 同号码重新接纳保留账号和业务资料，撤销旧登录身份，等待旧连接清理后启动新身份。
4. 运行服务取得 holder 租约后建立连接；后台收取、消息操作和状态查询共用连接，协议状态串行提交。
5. 主动退出保存登录状态并清理连接，账号资料、联系人和历史消息继续保留。进程重启后从 SQL 恢复有效账号。

部署采用单运行实例，添加服务独立进程运行。数据库及选定的运行态后端在启动时校验。

## 消息与运维

消息投影复用上游 Proto，保存引用、编辑、撤回和反应的目标关系。商家模板、按钮、列表及交互消息的可见正文沿用文本、敏感值脱敏和验证码提取链路。

账号级代理配置随账号材料接纳并持久化；修改配置后刷新该账号连接。拒登账号可在详情页查询与提交复核，复核状态独立于登录状态保存。

已有部署按[迁移说明](../migration.md)完成停写、备份、离线转换和两服务统一切换。设备 worker 的构建与运行分别见 [Android](../../tools/android-attestation/README.md)和 [iOS](../../tools/ios-attestation/README.md)。

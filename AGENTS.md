# AGENTS.md

## 边界

- `wa-app` 负责 WA 应用链路的应用层建模、原子 RPC 服务和运行时适配。
- Proto 是本目录领域模型、RPC 请求/响应、状态、错误码和事件语义的唯一真源。
- 不直接 import `wa-reverse` 或其他 sibling 目录源码；后续实现只能通过发布包、RPC、配置边界或人工迁移后的服务内实现协作。
- 不在契约中暴露具体代理地址、endpoint URL、数据库表名、Valkey key、脚本路径、APK 文件路径等实现细节。

## 数据与安全

- 账号运行服务的长期事实、凭证、协议状态、消息元数据、审计投影和接纳回执显式选择 PostgreSQL 或 SQLite；短期缓存、会话存活和所有权租约默认使用进程内存，可显式选择 Valkey。
- 添加服务的短流程、验证材料和待交付结果保存在进程内存，完成、取消或超时后清理；接纳后的账号与恢复状态由账号运行服务持久化。
- 实现层可以定义本服务自有 SQL schema 与 Valkey key；这些细节不得进入 proto 契约或跨仓公共模型。
- OTP、Flag、token、authkey、identity/prekey、session、cookie、可复用请求材料都视为敏感数据；文档和日志不得输出真实值。
- Linter 检查必须达到 0 error / 0 warning；禁止通过修改或放宽 linter 配置、降低规则级别、删除规则、添加 ignore/disable/nolint/ts-ignore/eslint-disable/biome-ignore/prettier-ignore 等方式绕过问题，只能按 linter 规则修复源码、类型、格式或依赖边界。

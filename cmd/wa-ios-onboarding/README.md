# iOS 注册联调

命令行工具在 Go 进程管理注册流程，通过独立 [App Attest worker](../../tools/ios-attestation/README.md)调用设备能力。流程与验证材料保存在内存，退出时清理；正式账号交付使用账号添加服务。

## 输入

号码文件使用 [PhoneTarget](../../proto/byte/v/forge/waapp/v1/common.proto)的 Proto JSON，权限为 0600。

~~~sh
go run ./cmd/wa-ios-onboarding \
  -target 127.0.0.1:50194 \
  -phone "$IOS_PHONE_TARGET"
~~~

## 流程控制

标准输入每行一条 JSON：

~~~json
{"operation":"probe"}
{"operation":"request_code"}
{"operation":"submit_code","otp_file":"/private/flow/fresh-otp.json"}
{"operation":"status"}
{"operation":"stop"}
~~~

| 操作 | 行为 |
| --- | --- |
| probe | 准备本流程证明并探测号码 |
| request_code | 请求短信，按服务端等待时间控制后续操作 |
| submit_code | 从受限文件读取验证码并提交 |
| status | 读取最近一次业务响应 |
| stop | 释放流程内存和证明上下文 |

交互进程最长运行 15 分钟。传输结果未知时保留流程状态，调用方根据最近结果决定后续操作。

验证码文件包含 code、RFC3339 格式的 received_at、fresh=true 和 number_matches=true。调用端检查文件权限、匹配标记与接收时间，输出固定状态、验证码长度和冷却秒数。

## 代码与检查

internal/waapp/protocol/onboarding/ios 实现流程、参数和传输，internal/waapp/protocol/attestation 提供 worker 客户端。

~~~sh
go test -race ./internal/waapp/protocol/... ./cmd/wa-ios-onboarding
scripts/lint-go.sh
~~~

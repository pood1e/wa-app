# iOS App Attest worker

独立 Python gRPC worker 为账号添加流程提供 App Attest 和 reCAPTCHA SDK 能力。契约见 [app_attest.proto](../../proto/byte/v/forge/waapp/v1/app_attest.proto)与 [recaptcha.proto](../../proto/byte/v/forge/waapp/v1/recaptcha.proto)。账号密钥、验证输入和注册流程由 Go 服务管理。

## 构建与检查

在仓库根目录执行：

~~~sh
uv run --no-project --with grpcio-tools==1.84.0 python tools/ios-attestation/generate.py
uv sync --frozen --project tools/ios-attestation
uv run --project tools/ios-attestation pytest tools/ios-attestation/tests -q
node --test tools/ios-attestation/tests/test-agent.mjs tools/ios-attestation/tests/test-recaptcha.mjs
uv run --project tools/ios-attestation ruff check tools/ios-attestation/src tools/ios-attestation/tests tools/ios-attestation/generate.py
uv build --project tools/ios-attestation --wheel --out-dir .tmp/ios-attestation-dist
~~~

wheel 包含 worker、设备适配及生成的 Python Proto；依赖固定于 uv.lock。

## 运行与联调

Mac 使用与设备 frida-server 匹配的客户端版本，连接已配对且已准备开发者镜像的设备，并保持 WhatsApp 进程运行。安装 wheel 后启动：

~~~sh
wa-ios-attestation --device "$IOS_DEVICE_ALIAS" --socket "$IOS_ATTEST_SOCKET"
~~~

设备别名为 Frida USB device ID 的 SHA-256 十六进制前 10 位。socket 使用绝对路径，父目录由当前用户拥有且权限为 0700，socket 权限为 0600。

跨主机通过 SSH 转发：

~~~sh
ssh -NT -o ExitOnForwardFailure=yes \
  -L "127.0.0.1:50194:$IOS_ATTEST_SOCKET" "$IOS_ATTEST_HOST"
go run ./cmd/wa-appattest-check -target 127.0.0.1:50194
~~~

同机可使用 -target "unix://$IOS_ATTEST_SOCKET"。检查工具创建一次合成流程并验证证明、签名和释放；日常探活使用标准 gRPC Health。注册联调见 [wa-ios-onboarding](../../cmd/wa-ios-onboarding/README.md)。

每台设备运行一个 worker，硬件操作串行执行。最多保留 32 个流程上下文，30 分钟后过期；释放、过期或重启清理内存状态。取消与超时会等待设备操作清理。

## 代码导航

| 位置 | 职责 |
| --- | --- |
| src/wa_ios_attestation/app.py | Proto gRPC、流程隔离与生命周期 |
| src/wa_ios_attestation/native.py | 原生调用、超时和取消 |
| src/wa_ios_attestation/execution.py / session.py | 设备执行与会话清理 |
| src/wa_ios_attestation/attestation.py | 证明和签名校验 |
| src/wa_ios_attestation/agent.js | App Attest SDK 适配 |
| src/wa_ios_attestation/recaptcha.py / recaptcha.js | reCAPTCHA 服务与 SDK 适配 |
| generate.py | 从同一 Proto 生成 Python 类型 |

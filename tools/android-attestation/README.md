# Android 签名服务

独立 Python gRPC worker 为账号添加流程提供 AndroidKeyStore 证明、签名与 Play Integrity Standard 能力。契约见 [android_attestation.proto](../../proto/byte/v/forge/waapp/v1/android_attestation.proto)。账号、验证码和注册流程由 Go 服务管理。

## 构建与运行

需要 USB 连接的 Android 设备、主机 ADB、已运行的 WhatsApp 及匹配版本的 Frida server。在本目录执行：

~~~sh
uv run --no-project --with grpcio-tools==1.84.0 python generate.py
uv sync --frozen --group dev
uv run pytest
uv run ruff check src tests
uv run wa-android-attestation --socket "$ANDROID_ATTEST_SOCKET" --device "$ANDROID_DEVICE_ALIAS"
~~~

设备别名为 Frida USB device ID 的 SHA-256 十六进制前 10 位。socket 使用绝对路径，父目录权限为 0700，socket 权限为 0600。

添加服务通过私有 Unix socket 或 SSH loopback 转发连接，配置 WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET。每台设备串行执行硬件调用，取消与超时会回收操作进程。流程上下文在结束或 30 分钟到期后清理。

标准 gRPC Health 用于日常探活，设备能力由实际 SDK 调用确认。证明能力按配置与设备可用性启用。

## 代码导航

| 位置 | 职责 |
| --- | --- |
| src/wa_android_attestation/app.py | Proto gRPC、流程状态和生命周期 |
| src/wa_android_attestation/native.py | 设备调用与取消清理 |
| src/wa_android_attestation/agent.js | 设备 SDK 适配 |
| generate.py | 从同一 Proto 生成 Python 类型 |

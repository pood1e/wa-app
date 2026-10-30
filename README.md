# wa-app

自托管 WhatsApp 账号管理与消息服务，提供 Dashboard、Proto gRPC 和同源 HTTP API。

## 架构与功能

- wa-account-add-service：Android / iOS 账号注册、旧设备验证码转移、两步验证密码和账号交付。
- wa-app-service：账号接纳、登录恢复、消息、联系人、群管理、账号设置及 Dashboard。
- 长期数据显式选择 PostgreSQL 或 SQLite；短期缓存与连接租约默认使用内存，可选 Valkey。
- Proto 统一定义领域模型、RPC 和私有恢复状态；设备 SDK 能力通过独立 worker 接入。

Dashboard 提供账号代理、重新登录、账号复核、邮箱验证、联系人与群资料管理、聊天分页、未读计数和消息回执。消息支持文本、商家模板正文、引用、编辑、撤回和反应；媒体消息展示类型与说明。

服务依赖、数据生命周期和代码导航见[架构说明](docs/architecture/README.md)。

## 快速启动

需要 Git、Docker Compose v2 和 OpenSSL。已有数据库先按[迁移说明](docs/migration.md)完成停写、备份和离线转换。

~~~sh
git clone git@github.com:pood1e/wa-app.git
cd wa-app
cat > .env <<EOF
WA_SERVICE_TOKEN=$(openssl rand -hex 32)
WA_APP_STORE=sqlite
EOF
docker compose up -d --build --wait
~~~

Dashboard：<http://127.0.0.1:8080>。SQLite 数据保存在 wa-app-data 命名卷，容器内路径为 /var/lib/wa-app/wa-app.sqlite3。

~~~sh
docker compose ps
docker compose logs -f wa-app wa-account-add
docker compose down
~~~

两服务共享 WA_SERVICE_TOKEN。Compose 将 Dashboard 与运行服务 gRPC 绑定到本机，添加服务通过容器网络提供 gRPC。

## 部署配置

配置写入根目录 .env，完整示例见 [.env.example](.env.example)。

| 变量 | 用途 |
| --- | --- |
| WA_SERVICE_TOKEN | 两服务共享认证，必填 |
| WA_APP_STORE | SQL 后端：sqlite 或 postgres，必填 |
| WA_APP_SQLITE_FILE | SQLite 文件路径 |
| WA_APP_PG_DSN / POSTGRES_PASSWORD | PostgreSQL 连接与 Compose 数据库密码 |
| WA_APP_EPHEMERAL_STORE | 短期状态：memory（默认）或 valkey |
| WA_APP_VALKEY_URL | Valkey 连接 |
| WA_APP_AUTH_PASSWORD | Dashboard 访问密码 |
| WA_COMMON_PROXY | 默认 HTTP CONNECT / SOCKS5 出站代理 |
| WA_ACCOUNT_ADD_TARGET / WA_ACCOUNT_RUNTIME_TARGET | 两服务 gRPC 地址，Compose 自动设置 |
| WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET / WA_ACCOUNT_ADD_IOS_SIGNER_TARGET | 设备 worker 地址 |
| WA_APP_IMAGE_TAG / WA_ACCOUNT_ADD_IMAGE_TAG | 两服务镜像标签，默认 latest |

PostgreSQL 部署设置 WA_APP_STORE=postgres 和 WA_APP_PG_DSN；使用 Compose 数据库时同时设置 POSTGRES_PASSWORD，连接主机为 postgres、用户和数据库为 wa_app。启动依赖后再启动两个服务：

~~~sh
docker compose --profile postgres up -d --wait postgres
docker compose --profile postgres up -d --build --wait wa-app wa-account-add
~~~

Valkey 部署设置 WA_APP_EPHEMERAL_STORE=valkey 与 WA_APP_VALKEY_URL=valkey://valkey:6379/0，然后启动对应 profile：

~~~sh
docker compose --profile valkey up -d --wait valkey
docker compose up -d --build --wait wa-app wa-account-add
~~~

账号级代理可在添加页指定或在详情页修改，优先于 WA_COMMON_PROXY；未配置代理时直连。代理凭证保存在 SQL，公开读取时脱敏。

设备 worker 的构建与连接见 [Android 签名服务](tools/android-attestation/README.md)和 [iOS App Attest worker](tools/ios-attestation/README.md)。目标需能从添加服务容器访问，证明能力按配置与设备可用性启用，上游响应决定账号流程结果。

### 发布镜像

| 服务 | 镜像 |
| --- | --- |
| 账号运行 | ghcr.io/pood1e/wa-app/wa-app-service |
| 账号添加 | ghcr.io/pood1e/wa-app/wa-account-add-service |

镜像支持 linux/amd64 与 linux/arm64。main 发布 latest 和 sha-<提交短哈希>，v* 标签发布对应版本。

~~~sh
docker compose pull wa-app wa-account-add
docker compose up -d --no-build --wait wa-app wa-account-add
~~~

## 开发与检查

需要 Go 1.27.2、Node.js 24 和含标准 Proto 定义的 protoc。Debian / Ubuntu 使用 protobuf-compiler libprotobuf-dev。

~~~sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
scripts/generate-proto.sh
npm --prefix webui ci
npm --prefix webui run proto
scripts/lint-go.sh
go test -race ./cmd/... ./internal/...
npm --prefix webui test
npm --prefix webui run lint
npm --prefix webui run build
~~~

WA_TEST_PG_DSN 与 WA_TEST_VALKEY_URL 用于真实后端集成测试，数据库测试使用独立临时 schema。[CI](.github/workflows/test.yml)执行同一组检查。

| 入口 | 内容 |
| --- | --- |
| [架构说明](docs/architecture/README.md) | 服务依赖、数据生命周期和代码导航 |
| [数据迁移](docs/migration.md) | 停写、备份、转换、切换与恢复 |
| [Proto 契约](proto/byte/v/forge/waapp) | 公共 API 与私有协议状态 |
| [iOS 注册联调](cmd/wa-ios-onboarding/README.md) | 命令行输入和流程控制 |

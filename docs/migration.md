# 数据与服务切换

本次切换同时更新两服务、Proto 契约和 Dashboard。长期数据可继续使用 PostgreSQL 或 SQLite；短期状态默认使用进程内存，Valkey 可选。
`wa-migrate` 在原数据库内执行一次性事务转换。运行服务发现旧原生状态或注册工作流表时拒绝启动。

## 停写与备份

1. 停止旧 `wa-app`，确认旧进程不再写数据库或持有聊天连接。
2. PostgreSQL 使用 `pg_dump` 备份；SQLite 使用 `sqlite3 "$WA_APP_SQLITE_FILE" ".backup '$SQLITE_BACKUP_FILE'"` 创建一致备份。
3. 保留旧镜像、原环境配置与备份，核对备份可恢复。数据库备份按凭证文件管理。

## 转换

配置 `WA_APP_STORE=postgres` 与 `WA_APP_PG_DSN`，或 `WA_APP_STORE=sqlite` 与 `WA_APP_SQLITE_FILE`。
SQLite 路径必须指向现用文件；Compose 沿用默认文件 `/var/lib/wa-app/wa-app.sqlite3`；自定义路径需要显式设置容器内路径。
执行新运行镜像中的迁移命令：

```sh
docker compose run --rm --no-deps --user 0:0 --entrypoint /app/wa-migrate wa-app
```

本地构建也可执行：

```sh
go run ./cmd/wa-migrate
```

转换保留账号 ID、登录标识、消息、联系人和审计数据。已完成账号的密钥与恢复状态转换为 Proto，写入后逐项回读核对；
SQLite 同时重建业务表并启用外键，校验账号、材料及消息关联。
缺失或非法密钥、重复活动登录态、关联不一致、SQL 错误及取消均导致事务回滚。
旧注册工作流、设备准备和临时运行表在同一事务移除；没有登录、会话或业务数据的未完成账号被清理。
转换后再次执行会报告数据库已非旧结构。

新镜像运行用户为 UID / GID `10001`。旧 SQLite 卷若由 root 创建，转换后将专属数据卷交给该用户：

```sh
docker compose run --rm --no-deps --user 0:0 --entrypoint /bin/sh wa-app \
  -c 'chown -R 10001:10001 /var/lib/wa-app'
```

旧未完成流程在新添加服务重新开始。SQLite 继续使用同一显式文件后端，本命令不转换数据库引擎。

## 启动与验收

1. 使用 PostgreSQL 时确认选定实例可连接。短期状态默认 `WA_APP_EPHEMERAL_STORE=memory`；选择 `valkey` 时设置 `WA_APP_VALKEY_URL` 并启动对应实例。
2. 更新同一版本的前端与两个服务，设置共享服务 token 和相互 gRPC 地址，启动运行服务及添加服务。
3. 核对账号数、登录状态、联系人与消息；验证已有账号收发和运行服务重启恢复。
4. 验证一次添加流程、取消和超时，停止添加服务后核对已有账号仍能运行。

内存或 Valkey 的短期缓存与租约由运行服务重建；账号、凭证、恢复状态和接纳回执保存在 SQL。
运行服务按单实例部署，添加服务没有数据库和持久卷。
Android 添加流程使用随协议打包的单一实测设备基线；机型池与外部覆盖文件已移除。发布时移除旧设备池配置，已有账号继续使用其持久化的客户端元数据。

回滚时先停止两个新服务及连接，再恢复完整数据库备份、旧镜像与旧配置。旧版本只能配合恢复后的旧数据库使用。

## 验证覆盖

两步验证设置契约已从六位 `pin` 改为 `SensitiveText password`，原字段号与名称保留。客户端重新生成 Proto，并与运行服务、Dashboard 一起发布。Canonical 私有凭证按需写入既有 `AccountCredentials`；已有账号使用原登录材料恢复。

`internal/waapp/store` 的 PostgreSQL 与 SQLite 集成测试覆盖接纳幂等、并发、不可变回执、事务回滚、取消及重启读取。
迁移测试覆盖已完成账号密钥、收发链与计数器保留、旧结构启动拒绝、非法材料回滚；session 测试覆盖租约丢失及删除前等待清理。
运行态测试覆盖内存并发所有权、续租与过期、数据拷贝及关闭清理，并保留真实 Valkey 所有者隔离测试。
正式数据转换与已有账号外网收发验收需要在停写窗口执行。

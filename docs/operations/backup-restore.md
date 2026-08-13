# 备份与恢复 Runbook

本文档描述琅嬛（Langhuan）的备份、恢复与灾难恢复流程，覆盖数据库、对象存储与配置三个维度。适用于 v1.0.0 前的内部部署；v1.0.0 起仍遵循本流程，但额外承诺跨版本数据迁移路径。

第 1–8 节面向 PostgreSQL + Redis 生产部署；v1.0.0 新增的 standalone 单机模式（SQLite，零外部依赖）备份恢复见 §9。

## 1. 需要备份什么

琅嬛的有状态组件：

| 组件 | 存储内容 | 备份方式 |
|------|---------|---------|
| PostgreSQL（含 pgvector） | 知识库、文档、分块、修订、索引代次、检索投影、用户/租户/凭证 | `pg_dump` 逻辑备份或基于基础镜像的物理备份 |
| Redis | asynq 任务队列、会话、限流计数 | 队列是临时态，通常无需备份（任务可重投）；会话丢失要求用户重新登录 |
| 对象存储（raw / parser / asset） | 原始上传文件、解析产物、图片资产 | `ossutil` / `aws s3 sync` / `rclone` 同步到备份桶 |
| 配置文件 `config.yaml` | 服务连接、模型参数、队列策略 | 与 `credentials.encryption_key` 一起安全保管，**不入版本库** |
| `credentials.encryption_key` | Provider 凭证与 API Key 的 AES-256 主密钥 | 独立保管；**丢失即不可解密已加密凭证** |

> 关键：`encryption_key` 丢失后，所有 `model_providers.credential_cipher` 和 `workspace_api_tokens.secret_cipher` 将永久不可解密。必须与数据库备份分开、独立安全保管（如 KMS、密码管理器、离线介质）。

> 以上面向 PostgreSQL 生产部署。standalone 单机模式（SQLite）的数据集中在 `~/.langhuan-data/`，备份恢复方式不同，见 §9。

## 2. 数据库备份

### 2.1 逻辑备份（pg_dump）

推荐日常备份方式，跨 PostgreSQL 小版本兼容：

```bash
# 完整备份（含 pgvector 扩展数据）。
# --no-owner --no-acl 避免恢复时角色冲突。
pg_dump \
  --host=127.0.0.1 --port=5432 \
  --username=langhuan --dbname=langhuan \
  --format=custom --no-owner --no-acl \
  --file=langhuan-$(date +%Y%m%d-%H%M%S).dump
```

建议频率：**每日一次**，保留 7~30 天滚动窗口。

### 2.2 验证备份可恢复

定期（如每周）在临时环境验证备份可恢复：

```bash
# 启动临时 pgvector 容器（与生产同镜像）。
docker run -d --name langhuan-restore-test \
  -e POSTGRES_PASSWORD=test \
  -p 15432:5432 \
  langhuan-test-postgres:pg17

# 恢复。
pg_restore \
  --host=127.0.0.1 --port=15432 \
  --username=postgres --dbname=postgres \
  --create --no-owner --no-acl \
  langhuan-20260809.dump

# 验证关键表行数。
psql --host=127.0.0.1 --port=15432 --username=postgres --dbname=langhuan -c \
  "SELECT 'knowledge_bases' AS t, count(*) FROM knowledge_bases
   UNION ALL SELECT 'documents', count(*) FROM documents
   UNION ALL SELECT 'document_revisions', count(*) FROM document_revisions
   UNION ALL SELECT 'retrieval_entries', count(*) FROM retrieval_entries;"

# 清理。
docker rm -f langhuan-restore-test
```

## 3. 对象存储备份

### 3.1 S3-compatible（RustFS / MinIO / 阿里云 / 腾讯云）

```bash
# 用 aws-cli 同步到备份桶（适用于任何 S3-compatible 后端）。
export AWS_ENDPOINT_URL=http://your-storage-endpoint:9000
aws s3 sync s3://langhuan-raw      s3://langhuan-backup/raw      --no-progress
aws s3 sync s3://langhuan-parser   s3://langhuan-backup/parser   --no-progress
aws s3 sync s3://langhuan-assets   s3://langhuan-backup/assets   --no-progress
```

或用 `rclone`（跨后端兼容性更好）：

```bash
rclone sync storage:langhuan-raw    backup:langhuan-backup/raw
rclone sync storage:langhuan-parser backup:langhuan-backup/parser
rclone sync storage:langhuan-assets backup:langhuan-backup/assets
```

### 3.2 Local 模式

单机开发用 `storage.driver: local`，数据在 `storage.raw_document_dir`。直接打包目录：

```bash
tar -czf langhuan-data-$(date +%Y%m%d).tar.gz ./data/
```

## 4. 配置与密钥备份

```bash
# 备份 config.yaml（含连接参数，但不含 encryption_key 明文）。
cp config.yaml backup/config-$(date +%Y%m%d).yaml

# encryption_key 单独、加密保管（如放入密码管理器或 KMS）。
# 从 config.yaml 提取 base64 key：
grep encryption_key config.yaml
```

> 不要把 `encryption_key` 和 `config.yaml` 放在同一个备份介质里明文存储——分开保管降低同时泄露的风险。

## 5. 完整恢复流程

假设数据库故障，从备份恢复到新实例：

```bash
# 1. 启动新的 PostgreSQL + pgvector 实例（确保扩展可用）。
#    创建空数据库 langhuan 和角色 langhuan。

# 2. 恢复数据库。
pg_restore \
  --host=NEW_HOST --port=5432 \
  --username=langhuan --dbname=langhuan \
  --no-owner --no-acl \
  langhuan-20260809.dump

# 3. 恢复对象存储（如使用 S3）。
aws s3 sync s3://langhuan-backup/raw    s3://langhuan-raw
aws s3 sync s3://langhuan-backup/parser s3://langhuan-parser
aws s3 sync s3://langhuan-backup/assets s3://langhuan-assets

# 4. 部署 config.yaml，恢复 encryption_key（务必与备份时一致）。
#    更新 database.dsn / redis.addr / storage.s3 指向新实例。

# 5. 启动琅嬛（迁移会自动应用到恢复后的 schema）。
./langhuan

# 6. 验证：通过 Web Console / REST 检索一个已知文档，确认 retrieval_entries 可命中。
curl http://NEW_HOST:8080/api/v1/healthz
```

## 6. 灾难恢复演练（smoke）

在干净的 docker 环境（只有 docker，无预置 PostgreSQL）完整跑一遍：

```bash
# 1. 临时环境导入文档 → 等待 completed。
# 2. pg_dump 备份。
# 3. drop 数据库。
# 4. pg_restore 恢复。
# 5. 重启琅嬛，执行检索验证 active Generation 与 retrieval_entries 完整。
# 6. 确认加密凭证可解密（模型连接测试通过）。
```

通过标准：

- 恢复后检索能命中备份时已索引的文档。
- active Generation 状态正确（ready），retrieval_entries 行数与备份一致。
- Provider 凭证可解密（encryption_key 一致的前提下）。

## 7. v1.0.0 前的注意事项

- v1.0.0 前仅支持全新安装，不承诺历史测试 schema 的数据迁移。备份恢复演练用空库，恢复即"重建"。
- schema 变更（迁移）在恢复后由 `migrate.Run` 应用；若备份来自更旧的 schema 版本，恢复后需确认所有迁移成功执行。
- Redis 无需备份：asynq 任务可重投（失败任务可通过 v0.8.0 的重试入口恢复），会话丢失仅要求重新登录。

## 8. 监控备份健康

建议接入告警：

- 每日备份任务的成功/失败通知。
- 备份文件大小环比异常（骤降可能意味着备份不完整）。
- 定期恢复验证（第 2.2 节）的通过/失败。
- 对象存储同步任务的成功/失败。

## 9. Standalone 单机模式（SQLite）备份与恢复

v1.0.0 起琅嬛支持零配置 standalone 单机模式：单个二进制 + 单个 SQLite 数据库文件，默认数据目录在 `~/.langhuan-data/`，不依赖 PostgreSQL / Redis / S3。该模式的备份恢复与上面 PostgreSQL 部署完全不同——所有可恢复状态都集中在本地文件系统，备份即“打包数据目录”。设计与合同详见 `docs/superpowers/specs/2026-08-11-sqlite-zero-config-standalone-design.md` §2.3、§2.4 与 §14。

### 9.1 需要备份的文件

standalone 模式下，`~/.langhuan-data/` 内以下四个文件 / 目录必须一起备份，缺一不可完整恢复：

| 路径 | 内容 | 丢失后果 |
|------|------|---------|
| `langhuan.db` | SQLite 数据库（知识库、文档、分块、修订、索引代次、检索投影、向量 BLOB、FTS5 索引、用户 / 租户 / 凭证密文） | 数据丢失 |
| `raw-documents/` | 原始上传文件、解析产物、图片资产（local 存储目录） | 原始文件丢失；已索引内容仍可检索，但无法追溯原文 |
| `credential.key` | Provider 凭证 / API Key / 飞书连接 / Workspace API Token 的 AES-256 主密钥 | **密文永久不可恢复**，所有加密凭证作废 |
| `config.yaml` | standalone 兜底配置（运行参数、`encryption_key_file` 指向 credential.key） | 可重建（见 §9.4），但建议备份以保留用户自定义 |

> 关键：`credential.key` 不可丢失。它是 32 字节加密随机密钥的 Base64 文本，standalone 首次启动生成后**绝不自动轮换或覆盖**。一旦丢失，数据库内 `model_providers.credential_cipher`、`workspace_api_tokens.secret_cipher` 和飞书连接 `credentials_ciphertext` 将永久不可解密。首次启动生成后必须立即备份。

### 9.2 credential.key 与数据库分文件的意义

`credential.key` 单独成文件（而非内嵌进 `config.yaml`）是有意的隔离设计：

- `config.yaml` 是用户可编辑、可分享的非敏感配置，通过 `credentials.encryption_key_file` 字段指向密钥文件路径。
- 密钥文件单一职责，用户几乎不会触碰，降低误删 / 误传概率。
- **分文件至少保留“仅数据库文件泄漏”时的隔离**：如果只有 `langhuan.db` 被窃取（如备份介质部分外泄），攻击者拿到的是加密密文，没有主密钥仍无法解密凭证。
- 但**整包（`langhuan.db` + `credential.key` + 数据目录）泄漏不提供静态加密防护**——主密钥与密文同时可得，凭证可被解密。因此备份介质必须整体加密、访问受控，不能假定“数据库文件本身已加密”。

### 9.3 备份操作

standalone 备份即原子地打包整个数据目录。建议停服务后冷备份，或用 SQLite 的 `.backup` 在线生成一致性快照，避免备份到正在写入的 WAL 中段：

```bash
# 方式一：停服务后冷备份（最简单、最一致）。
systemctl stop langhuan   # 或停止占用 .db 的进程
tar -czf langhuan-standalone-$(date +%Y%m%d-%H%M%S).tar.gz \
  -C "$HOME" .langhuan-data/
systemctl start langhuan

# 方式二：在线热备份（服务不停，先用 sqlite3 .backup 生成一致性 DB 快照）。
sqlite3 "$HOME/.langhuan-data/langhuan.db" ".backup '$HOME/backup-langhuan.db'"
tar -czf langhuan-standalone-$(date +%Y%m%d-%H%M%S).tar.gz \
  -C "$HOME" \
  backup-langhuan.db \
  .langhuan-data/raw-documents \
  .langhuan-data/credential.key \
  .langhuan-data/config.yaml
rm -f "$HOME/backup-langhuan.db"
```

无论哪种方式，四个文件 / 目录（`langhuan.db`、`raw-documents/`、`credential.key`、`config.yaml`）都必须落在同一份备份中，且备份介质整体加密存储。`credential.key` 不要与其它文件脱节单独存放——恢复时缺它即不可解密。

### 9.4 恢复操作

SQLite 零外部依赖，恢复就是把四个文件 / 目录放回 `~/.langhuan-data/` 后重启进程：

```bash
# 1. 停止现有 langhuan 进程，确保无进程占用 .db。
systemctl stop langhuan

# 2. 把备份内容放回（如有当前数据先移走或清空）。
mkdir -p "$HOME/.langhuan-data"
tar -xzf langhuan-standalone-20260809.tar.gz -C "$HOME"

# 3. 确认四个文件 / 目录齐全且权限正确。
ls -la "$HOME/.langhuan-data/"
#   langhuan.db        (-rw-------, 0600)
#   credential.key     (-rw-------, 0600)
#   config.yaml        (-rw-------, 0600)
#   raw-documents/     (drwx------, 0700)

# 4. 直接启动 langhuan（自动迁移会应用到恢复后的 schema）。
#    不需要准备 PostgreSQL / Redis / S3。
./langhuan

# 5. 验证：检索一个已知文档，确认向量与全文检索命中；测试 Provider 凭证可解密。
curl http://127.0.0.1:8080/api/v1/healthz
```

`config.yaml` 即使缺失也可重建：启动检测到 `~/.langhuan-data/` 下已有 `credential.key` 时会复用该密钥、只重新生成 config（详见 spec §2.4.3 删除组合表）。但建议连同备份，以保留用户自定义的 embedding / parser 配置。

### 9.5 standalone 模式的额外注意事项

- **内存队列不持久化**：standalone 无 Redis 时，asynq 队列替换为进程内有界优先队列。进程崩溃或重启时，尚未执行的导入 / 索引 / 同步任务会丢失，不会自动重放。这些任务由两条路径恢复：①进程内的 source cleanup / force latch 周期扫描补偿（飞书同步等可从 Job payload 安全重建的任务）；②用户对失败 / 未完成 Document 重新触发 retry / reindex。因此备份的是已完成落库的事实，队列中的临时态不在备份范围。
- **SQLite 写串行**：单写锁（`SetMaxOpenConns(1)` + `_txlock=immediate`）保证并发正确性，但单库写入吞吐有上限。备份恢复不改变该约束。
- **不要跨 driver 迁移**：SQLite 与 PostgreSQL 数据文件不互相升级。从 standalone 切到 PostgreSQL 需要重新导入文档，不能直接搬运 `.db`。
- **权限校验**：恢复后若 `credential.key` / `config.yaml` 权限过宽（非 `0600`）或数据目录非 `0700`，langhuan 会在启动时尝试收紧，收紧失败则拒绝启动。从备份介质解压后注意恢复权限。

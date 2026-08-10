# 备份与恢复 Runbook

本文档描述琅嬛（Langhuan）的备份、恢复与灾难恢复流程，覆盖数据库、对象存储与配置三个维度。适用于 v1.0.0 前的内部部署；v1.0.0 起仍遵循本流程，但额外承诺跨版本数据迁移路径。

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

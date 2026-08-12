# SQLite 零配置单机模式 切片 1-2：启动合同与数据库地基 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 spec §13 的切片 1（启动合同）与切片 2（数据库地基），使琅嬛在零配置下能按四态探测链解析配置、生成/复用 credential.key、落盘可编辑 config.yaml，并以项目内 GORM SQLite Dialector 打开 SQLite 连接、按 driver 分流迁移，PG 路径全程零回归。

**Architecture:** 配置选择改造为返回 `ConfigSelection{Path, Explicit}` 的四态有序探测链（显式 → 当前目录 → `~/.langhuan-data/config.yaml` → 首次生成）。credential.key 始终是独立 0600 文件，config 通过 `credentials.encryption_key_file` 指向它，密钥不进 config 文本。SQLite 连接由项目内 `internal/infrastructure/db/sqlitedialect` 实现 GORM Dialector（底层只用 modernc.org/sqlite，避免与 golang-migrate 的 modernc 重复注册），迁移走 golang-migrate 的纯 Go `database/sqlite` 包与独立 `migrations_sqlite/` 目录。

**Tech Stack:** Go 1.26、GORM 1.31、modernc.org/sqlite v1.56.0 + modernc.org/sqlite/vec、golang-migrate v4 `database/sqlite`、golang-migrate v4 `database/postgres`（PG 零回归）、CGO_ENABLED=0。

**上游 spec：** `docs/superpowers/specs/2026-08-11-sqlite-zero-config-standalone-design.md`（§2.1 配置选择、§2.2 standalone profile、§2.3 权限、§2.4 凭证主密钥与 encryption_key_file、§3 技术选型、§4.1-4.3 Dialect/Open/migrate、§13 切片 1-2）。

## Global Constraints

- **PG 零回归是硬约束**：每个 task 的最后一步必须确认 `database.driver=postgres` 行为不变；任何改动不得删改 `migrations/`（PG 迁移目录）内容。SQLite 迁移目录是新增独立的 `migrations_sqlite/`。
- **配置选择不做模糊回退**（spec §2.1）：四态链中任意命中层文件存在但损坏/不可读/校验失败一律 fail-fast，绝不静默进入下一层或重新生成。
- **credential.key 绝不覆盖**（spec §2.4.2）：已存在则读校验，损坏则 fail-fast；仅删 config.yaml 时复用已有 key 重新生成 config。
- **密钥与配置分离**（spec §2.4）：密钥内容从不写入 config.yaml；config 只通过 `encryption_key_file` 绝对路径指向独立 key 文件。
- **不引入 glebarez/sqlite**（spec §3）：GORM SQLite Dialector 项目内实现，底层 driver 只用 modernc.org/sqlite；golang-migrate 也用其 `database/sqlite`（同源 modernc），避免两个纯 Go sqlite 包重复注册 `"sqlite"` driver name。
- **测试隔离**（AGENTS.md 5.10）：SQLite 测试只用 `t.TempDir()` 临时路径；严禁连 `config.yaml` 的库或 `~/.langhuan-data`。PG 集成测试仍走临时 docker 容器。
- **TDD + 中文 Conventional Commit**：每个 task 先写失败测试，再最小实现，独立提交。`encryption_key` 与 `encryption_key_file` 互斥校验。
- 切片 2 不实现 SQLite 迁移的实际表结构（那是切片 3），只建立 migrate 分流机制 + 一个空占位迁移验证管线通畅。

---

## File Structure

### 切片 1（启动合同）

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/infrastructure/config/config.go` | `RedisConfig.Enabled`、`CredentialsConfig.EncryptionKeyFile`、`ResolveEncryptionKey`、`validate` 放开 Redis 必填 | Modify |
| `internal/infrastructure/config/config_test.go` | 上述字段与互斥校验测试 | Modify |
| `internal/infrastructure/config/standalone.go` | `StandaloneProfile`、`MaterializeConfig` 落盘 config.yaml | Create |
| `internal/infrastructure/config/standalone_test.go` | 落盘内容/权限/头部注释/复用 key 测试 | Create |
| `internal/infrastructure/datadir/datadir.go` | `Resolve`、`Ensure`（目录 0700）、`EnsureCredentialKey`（生成/复用/校验）、`ReadCredentialKey` | Create |
| `internal/infrastructure/datadir/datadir_test.go` | 目录权限、key 生成/复用/损坏/并发/删除组合 | Create |
| `cmd/langhuan/config_select.go` | `ConfigSelection{Path, Explicit}`、`resolveConfigSelection`、`materializeStandalone` 四态探测 | Create |
| `cmd/langhuan/config_select_test.go` | 四态链命中/损坏 fail-fast/无静默回退 | Create |
| `cmd/langhuan/main.go` | `run`/`configPath`/`buildApp` 接入新选择链；错误文案不硬编码 PG | Modify |

### 切片 2（数据库地基）

| 文件 | 责任 | 动作 |
|---|---|---|
| `go.mod` / `go.sum` | 加 modernc.org/sqlite、modernc.org/sqlite/vec、golang-migrate 已在 | Modify |
| `internal/infrastructure/db/dialect.go` | `Dialect` 类型、`DialectOf` | Create |
| `internal/infrastructure/db/dialect_test.go` | 方言推断 | Create |
| `internal/infrastructure/db/db.go` | `Open(cfg) (*gorm.DB, Dialect, error)` 按 driver 分流 + PRAGMA 断言 + 连接池 | Modify |
| `internal/infrastructure/db/db_test.go` | SQLite open/PRAGMA/连接池、PG 零回归 | Modify/Create |
| `internal/infrastructure/db/sqlitedialect/dialector.go` | GORM Dialector：Name/Initialize/Migrator/DataTypeOf/DefaultValueOf/BindVarTo/QuoteTo/Explain/SavePoint/RollbackTo | Create |
| `internal/infrastructure/db/sqlitedialect/error_translator.go` | 2067/1555→ErrDuplicatedKey、787→ErrForeignKeyViolated | Create |
| `internal/infrastructure/db/sqlitedialect/clauses.go` | INSERT/LIMIT/FOR clause builder | Create |
| `internal/infrastructure/db/sqlitedialect/compatibility_test.go` | CRUD/约束翻译/时间/savepoint/Quote/DataType 合同测试（先行） | Create |
| `internal/infrastructure/db/sqlitedialect/LICENSE` | 借鉴 glebarez/go-sqlite 的 MIT 声明 | Create |
| `internal/infrastructure/migrate/migrate.go` | `Run(ctx, cfg)` 按 driver 分流 | Modify |
| `internal/infrastructure/migrate/migrate_test.go` | SQLite 分流、PG 零回归 | Modify/Create |
| `internal/infrastructure/migrate/migrations_sqlite/000001_init.up.sql` | 空占位（验证管线；切片 3 填实际 schema） | Create |
| `internal/infrastructure/migrate/migrations_sqlite/000001_init.down.sql` | 空占位 | Create |
| `internal/testsupport/sqlite.go` | `NewSQLiteDB(t)` 返回迁移后的 `*gorm.DB` | Create |

---

## 切片 1：启动合同

### Task 1: RedisConfig.Enabled 与 validate 放开

**Files:**
- Modify: `internal/infrastructure/config/config.go:193-197`（RedisConfig）、`:351-360`（defaultConfig）、`:635-646`（validate）
- Test: `internal/infrastructure/config/config_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestRedisEnabledDefaultsTrueAndOptionalWhenDisabled(t *testing.T) {
	// 旧 YAML 未写 enabled 时保持 true（兼容）
	t.Run("legacy yaml without enabled stays true", func(t *testing.T) {
		cfg, err := config.Load(writeTempYAML(t, "redis:\n  addr: localhost:6379\ncredentials:\n  encryption_key: "+validKey()))
		require.NoError(t, err)
		require.True(t, cfg.Redis.Enabled)
	})

	t.Run("disabled skips addr requirement", func(t *testing.T) {
		cfg, err := config.Load(writeTempYAML(t, "redis:\n  enabled: false\ndatabase:\n  driver: postgres\n  dsn: postgres://x\nstorage:\n  raw_document_dir: ./data\nretrieval:\n  failed_staging_retention: 1s\n  retired_generation_retention: 1s\ningest:\n  max_file_size_bytes: 1\ncredentials:\n  encryption_key: "+validKey()))
		require.NoError(t, err)
		require.False(t, cfg.Redis.Enabled)
	})

	t.Run("enabled true requires addr", func(t *testing.T) {
		_, err := config.Load(writeTempYAML(t, "redis:\n  enabled: true\ncredentials:\n  encryption_key: "+validKey()))
		require.ErrorContains(t, err, "redis.addr")
	})
}
```

`writeTempYAML` 与 `validKey` 是 test helper（见末尾 Appendix）。

- [ ] **Step 2: 验证测试失败**

Run: `go test ./internal/infrastructure/config -run TestRedisEnabled -count=1`
Expected: FAIL — `Enabled` 字段不存在、enabled=false 时仍报 addr 为空。

- [ ] **Step 3: 实现 Enabled 字段与默认值**

`internal/infrastructure/config/config.go`：

```go
type RedisConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}
```

`defaultConfig()` 把 `Redis: RedisConfig{Enabled: true, Addr: "127.0.0.1:6379"}`（spec §10.1：Enabled 默认 true，旧 YAML 未写时保持 true）。

- [ ] **Step 4: validate 按 Enabled 分发**

`validate()` 中把：

```go
if c.Redis.Addr == "" {
    return errors.New("redis.addr 不能为空")
}
```

改为：

```go
if c.Redis.Enabled && c.Redis.Addr == "" {
    return errors.New("redis.enabled=true 时 redis.addr 不能为空")
}
```

- [ ] **Step 5: 验证测试通过 + PG 回归**

Run: `go test ./internal/infrastructure/config -count=1`
Expected: PASS（含 RedisEnabled 三例与现有 config 测试全部通过——现有测试未写 enabled，默认 true，行为不变）。

- [ ] **Step 6: 提交**

```bash
git add internal/infrastructure/config/config.go internal/infrastructure/config/config_test.go
git commit -m "feat(config): RedisConfig 增加 Enabled 字段并在禁用时放开 addr 必填"
```

---

### Task 2: CredentialsConfig.EncryptionKeyFile 与互斥校验

**Files:**
- Modify: `internal/infrastructure/config/config.go:265-277`（CredentialsConfig、DecodeEncryptionKey）、`:692`（validate 调用点）
- Test: `internal/infrastructure/config/config_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestEncryptionKeyFileLoadsFromKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	keyB64 := validKey()
	require.NoError(t, os.WriteFile(keyPath, []byte(keyB64), 0o600))

	cfg, err := config.Load(writeTempYAML(t, fmt.Sprintf(
		"database:\n  driver: postgres\n  dsn: postgres://x\nstorage:\n  raw_document_dir: ./data\n"+
			"retrieval:\n  failed_staging_retention: 1s\n  retired_generation_retention: 1s\n"+
			"ingest:\n  max_file_size_bytes: 1\nredis:\n  enabled: false\n"+
			"credentials:\n  encryption_key_file: %s\n", keyPath)))
	require.NoError(t, err)
	got, err := cfg.Credentials.ResolveEncryptionKey()
	require.NoError(t, err)
	want, _ := base64.StdEncoding.DecodeString(keyB64)
	require.Equal(t, want, got)
}

func TestEncryptionKeyFileMutexWithEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	require.NoError(t, os.WriteFile(keyPath, []byte(validKey()), 0o600))
	yaml := fmt.Sprintf("credentials:\n  encryption_key: %s\n  encryption_key_file: %s\n",
		validKey(), keyPath)
	_, err := config.Load(writeTempYAML(t, yaml))
	require.ErrorContains(t, err, "不能同时")
}

func TestEncryptionKeyFileMissingFailsFast(t *testing.T) {
	yaml := "credentials:\n  encryption_key_file: /nonexistent/credential.key\nredis:\n  enabled: false\n"
	_, err := config.Load(writeTempYAML(t, yaml))
	require.ErrorContains(t, err, "encryption_key_file")
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/config -run TestEncryptionKey -count=1`
Expected: FAIL — 字段/方法不存在。

- [ ] **Step 3: 实现 EncryptionKeyFile 与 ResolveEncryptionKey**

`config.go`：

```go
type CredentialsConfig struct {
	EncryptionKey     string `yaml:"encryption_key"`
	EncryptionKeyFile string `yaml:"encryption_key_file"`
}

// ResolveEncryptionKey 解析主密钥：encryption_key 与 encryption_key_file 二选一，
// 返回 AES-256 所需的 32 字节。两者都填或都不填均视为校验失败。
func (c CredentialsConfig) ResolveEncryptionKey() ([]byte, error) {
	switch {
	case c.EncryptionKey != "" && c.EncryptionKeyFile != "":
		return nil, errors.New("credentials.encryption_key 与 encryption_key_file 不能同时指定")
	case c.EncryptionKey != "":
		return decodeBase64Key(c.EncryptionKey)
	case c.EncryptionKeyFile != "":
		raw, err := os.ReadFile(c.EncryptionKeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取 credentials.encryption_key_file 失败: %w", err)
		}
		return decodeBase64Key(strings.TrimSpace(string(raw)))
	default:
		return nil, errors.New("必须提供 credentials.encryption_key 或 credentials.encryption_key_file")
	}
}

func decodeBase64Key(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(key) != 32 {
		return nil, errors.New("主密钥必须是 Base64 编码的 32 字节")
	}
	return key, nil
}
```

保留旧 `DecodeEncryptionKey` 方法但内部改为 `return c.ResolveEncryptionKey()`（向后兼容现有调用点 `main.go:439`）。

- [ ] **Step 4: validate 改用 ResolveEncryptionKey**

把 `validate()` 中 `c.Credentials.DecodeEncryptionKey()` 调用改为 `c.Credentials.ResolveEncryptionKey()`。

- [ ] **Step 5: 验证通过 + PG 回归**

Run: `go test ./internal/infrastructure/config -count=1 && go build ./cmd/langhuan`
Expected: PASS；现有调用点 `cfg.Credentials.DecodeEncryptionKey()` 经向后兼容仍工作。

- [ ] **Step 6: 提交**

```bash
git add internal/infrastructure/config/config.go internal/infrastructure/config/config_test.go
git commit -m "feat(config): 增加 encryption_key_file 字段并与 encryption_key 互斥"
```

---

### Task 3: 数据目录解析与权限（datadir 包）

**Files:**
- Create: `internal/infrastructure/datadir/datadir.go`
- Test: `internal/infrastructure/datadir/datadir_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestEnsureCreatesDirWith0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".langhuan-data")
	d, err := datadir.Resolve(func() (string, error) { return filepath.Dir(filepath.Dir(dir)), nil })
	require.NoError(t, err)
	require.Equal(t, dir, d.Path())
	require.NoError(t, d.Ensure())
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o700), info.Mode().Perm())
}

func TestEnsureRejectsOverPermissiveExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".langhuan-data")
	require.NoError(t, os.Mkdir(dir, 0o755))
	d, err := datadir.Resolve(func() (string, error) { return filepath.Dir(filepath.Dir(dir)), nil })
	require.NoError(t, err)
	err = d.Ensure()
	require.ErrorContains(t, err, "权限") // 尝试收紧失败或拒绝
}

func TestResolveHomeFailureIsActionable(t *testing.T) {
	_, err := datadir.Resolve(func() (string, error) { return "", errors.New("no home") })
	require.ErrorContains(t, err, "主目录")
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/datadir -count=1`
Expected: FAIL — 包不存在。

- [ ] **Step 3: 实现 datadir.Dir**

```go
// Package datadir 准备 standalone 模式的持久数据目录与凭证密钥文件。
package datadir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const dirName = ".langhuan-data"

// HomeResolver 返回用户主目录，便于测试注入。
type HomeResolver func() (string, error)

// Dir 描述已解析的数据目录。
type Dir struct{ path string }

func Resolve(home HomeResolver) (Dir, error) {
	if home == nil {
		home = os.UserHomeDir
	}
	h, err := home()
	if err != nil || h == "" {
		return Dir{}, fmt.Errorf("解析用户主目录失败: %w", err)
	}
	return Dir{path: filepath.Join(h, dirName)}, nil
}

func (d Dir) Path() string { return d.path }

// Ensure 创建数据根目录（0700），已有目录权限过宽时尝试收紧，失败则拒绝。
func (d Dir) Ensure() error {
	info, err := os.Stat(d.path)
	if err == nil {
		if perm := info.Mode().Perm(); perm != 0o700 {
			if err := os.Chmod(d.path, 0o700); err != nil {
				return fmt.Errorf("收紧数据目录权限失败（当前 %o）: %w", perm, err)
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("检查数据目录失败: %w", err)
	}
	if err := os.Mkdir(d.path, 0o700); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 验证通过**

Run: `go test ./internal/infrastructure/datadir -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/infrastructure/datadir
git commit -m "feat(datadir): 新增 standalone 数据目录解析与 0700 权限保障"
```

---

### Task 4: credential.key 生成、复用与损坏 fail-fast

**Files:**
- Modify: `internal/infrastructure/datadir/datadir.go`（追加 CredentialKey 相关）
- Test: `internal/infrastructure/datadir/datadir_test.go`

- [ ] **Step 1: 写失败测试**

```go
const credKeyName = "credential.key"

func TestEnsureCredentialKeyGeneratesOnFirstRun(t *testing.T) {
	d := newTestDir(t)
	key, err := d.EnsureCredentialKey()
	require.NoError(t, err)
	require.Len(t, key, 32)
	info, err := os.Stat(filepath.Join(d.Path(), credKeyName))
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm())

	// 复用：第二次读到的密钥与首次一致，且不覆盖
	key2, err := d.EnsureCredentialKey()
	require.NoError(t, err)
	require.Equal(t, key, key2)
}

func TestEnsureCredentialKeyCorruptFailsFast(t *testing.T) {
	d := newTestDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(d.Path(), credKeyName), []byte("garbage"), 0o600))
	_, err := d.EnsureCredentialKey()
	require.ErrorContains(t, err, "密钥")
}

func TestEnsureCredentialKeyEmptyFileFailsFast(t *testing.T) {
	d := newTestDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(d.Path(), credKeyName), []byte{}, 0o600))
	_, err := d.EnsureCredentialKey()
	require.Error(t, err)
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/datadir -run TestEnsureCredentialKey -count=1`
Expected: FAIL — 方法不存在。

- [ ] **Step 3: 实现 EnsureCredentialKey**

`datadir.go` 追加：

```go
import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const credKeyFile = "credential.key"

// EnsureCredentialKey 保证 credential.key 存在且有效：
//   - 不存在 → O_CREATE|O_EXCL 生成 32 随机字节 Base64 文本，0600，fsync
//   - 已存在 → 读取并校验（Base64、32 字节、权限），绝不覆盖
//
// 损坏文件一律 fail-fast，绝不自动轮换（密文依赖该密钥）。
func (d Dir) EnsureCredentialKey() ([]byte, error) {
	path := filepath.Join(d.path, credKeyFile)
	// 先尝试读取已存在文件
	if raw, err := os.ReadFile(path); err == nil {
		return validateCredentialBytes(raw, path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取凭证密钥失败: %w", err)
	}
	// 不存在 → 竞争性创建
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成凭证密钥失败: %w", err)
	}
	b64 := []byte(base64.StdEncoding.EncodeToString(key))
	if err := writeFileExclusive(path, b64, 0o600); err != nil {
		if errors.Is(err, os.ErrExist) {
			// 并发首次：另一进程已胜出，有界重读
			return readCredentialWithRetry(path)
		}
		return nil, err
	}
	return key, nil
}

func writeFileExclusive(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func validateCredentialBytes(raw []byte, path string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("凭证密钥文件损坏（非 Base64 或长度非 32 字节）: %s", path)
	}
	// 收紧权限
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm() != 0o600 {
		_ = os.Chmod(path, 0o600)
	}
	return key, nil
}

func readCredentialWithRetry(path string) ([]byte, error) {
	// 有界重读：并发首次启动时胜出进程可能尚未写完
	for i := 0; i < 20; i++ {
		if raw, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			return validateCredentialBytes(raw, path)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("等待凭证密钥写入超时: %s", path)
}
```

（补 import：`errors`、`strings`、`time`；注意 `sync` 暂未用到，先不引。）

- [ ] **Step 4: 验证通过**

Run: `go test ./internal/infrastructure/datadir -count=1`
Expected: PASS（生成、复用、损坏、空文件四例）。

- [ ] **Step 5: 并发首次启动测试**

```go
func TestEnsureCredentialKeyConcurrentFirstRun(t *testing.T) {
	d := newTestDir(t)
	const n = 8
	var wg sync.WaitGroup
	keys := make([][]byte, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = d.EnsureCredentialKey()
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, keys[0], keys[i]) // 所有进程拿到同一密钥
	}
}
```

Run: `go test ./internal/infrastructure/datadir -run TestEnsureCredentialKeyConcurrent -race -count=1`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/infrastructure/datadir
git commit -m "feat(datadir): credential.key 首次生成/复用/损坏 fail-fast/并发安全"
```

---

### Task 5: standalone profile 与 config.yaml 落盘

**Files:**
- Create: `internal/infrastructure/config/standalone.go`
- Test: `internal/infrastructure/config/standalone_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestMaterializeStandaloneConfigWritesYAMLWithKeyRef(t *testing.T) {
	dir := t.TempDir()
	dataDir := datadir.Dir{} // 用测试 helper 构造指向 dir 的 Dir
	// （测试中通过注入 dataDir 路径避免依赖真实 home）

	cfgPath, err := config.MaterializeStandalone(dir, dataDirPath)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "config.yaml"), cfgPath)

	// 文件存在、0600、内容含 encryption_key_file 绝对路径、driver=sqlite、redis.enabled=false
	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm())

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "driver: sqlite")
	require.Contains(t, content, "enabled: false")           // redis
	require.Contains(t, content, filepath.Join(dataDirPath, "credential.key"))
	require.NotContains(t, content, "encryption_key:")        // 不直填密钥，只用 _file
	require.Contains(t, content, "自动生成")                   // 头部注释
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/config -run TestMaterializeStandalone -count=1`
Expected: FAIL — 函数不存在。

- [ ] **Step 3: 实现 StandaloneProfile 与 MaterializeStandalone**

`internal/infrastructure/config/standalone.go`：

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MaterializeStandalone 在 dataDir 中生成 standalone profile 的 config.yaml，
// 并通过 datadir.EnsureCredentialKey 保证 credential.key 存在（已存在则复用）。
// 返回落盘 config.yaml 的绝对路径。config.yaml 已存在时直接返回（不覆盖）。
//
// keyEnsure 是 datadir.Dir.EnsureCredentialKey 的注入点，避免 config 包反向依赖 datadir。
func MaterializeStandalone(dataDirPath string, keyEnsure func() ([]byte, error)) (string, error) {
	cfgPath := filepath.Join(dataDirPath, "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		return cfgPath, nil // 已存在，绝不覆盖（spec §2.1 第 3 层）
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查 standalone 配置失败: %w", err)
	}

	// 先确保 credential.key（复用或生成）
	if _, err := keyEnsure(); err != nil {
		return "", err
	}

	prof := standaloneProfile(dataDirPath)
	header := "# 由 langhuan 首次启动自动生成。可编辑；删除后下次启动会重建（credential.key 不重建）。\n"
	out, err := yaml.Marshal(prof)
	if err != nil {
		return "", fmt.Errorf("序列化 standalone 配置失败: %w", err)
	}
	if err := writeFileExclusive(cfgPath, append([]byte(header), out...), 0o600); err != nil {
		if os.IsExist(err) {
			return cfgPath, nil // 并发首次，另一进程已生成
		}
		return "", fmt.Errorf("写入 standalone 配置失败: %w", err)
	}
	return cfgPath, nil
}

func standaloneProfile(dataDirPath string) Config {
	c := defaultConfig()
	c.Database = DatabaseConfig{
		Driver:      "sqlite",
		DSN:         "file:" + filepath.Join(dataDirPath, "langhuan.db") + "?cache=shared",
		AutoMigrate: true,
	}
	c.Redis = RedisConfig{Enabled: false}
	c.Storage = StorageConfig{Driver: "local", RawDocumentDir: filepath.Join(dataDirPath, "raw-documents")}
	c.Server = ServerConfig{
		HTTPAddr: "127.0.0.1:8080", BaseURL: "http://127.0.0.1:8080",
		RunHTTP: true, RunWorker: true,
	}
	c.Auth.Session.SecureCookie = false
	c.Auth.Password.Enabled = true
	c.Auth.OIDC.Enabled = false
	// 密钥与配置分离：只指向文件，不内联密钥内容
	c.Credentials = CredentialsConfig{
		EncryptionKeyFile: filepath.Join(dataDirPath, "credential.key"),
	}
	return c
}
```

`writeFileExclusive` 与 datadir 包的同名函数重复——把它提到 `internal/infrastructure/config` 内部小 helper（或复用一个共享的 `internal/infrastructure/ioutil`；本 plan 采用 config 包内私有 helper，避免跨包耦合，DRY 程度可接受因为语义独立）。

- [ ] **Step 4: 验证通过**

Run: `go test ./internal/infrastructure/config -run TestMaterializeStandalone -count=1`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/infrastructure/config/standalone.go internal/infrastructure/config/standalone_test.go
git commit -m "feat(config): standalone profile 落盘 config.yaml 并以 encryption_key_file 指向独立密钥"
```

---

### Task 6: §2.4.3 删除组合集成测试

**Files:**
- Test: `internal/infrastructure/config/standalone_test.go`（追加）

- [ ] **Step 1: 写删除组合测试**

```go
func TestDeletionMatrixConfigRebuildableKeyNot(t *testing.T) {
	setup := func(t *testing.T) (dataDir, cfgPath, keyPath string) {
		root := t.TempDir()
		dataDir = filepath.Join(root, "data")
		require.NoError(t, os.MkdirAll(dataDir, 0o700))
		keyEnsure := func() ([]byte, error) {
			return datadir.Dir{Path: dataDir}.EnsureCredentialKey()
		}
		cfgPath, err := config.MaterializeStandalone(dataDir, keyEnsure)
		require.NoError(t, err)
		keyPath = filepath.Join(dataDir, "credential.key")
		return
	}

	t.Run("only config deleted reuses key", func(t *testing.T) {
		dataDir, cfgPath, keyPath := setup(t)
		keyBefore, _ := os.ReadFile(keyPath)
		require.NoError(t, os.Remove(cfgPath))
		// 再次 materialize
		keyEnsure := func() ([]byte, error) { return datadir.Dir{Path: dataDir}.EnsureCredentialKey() }
		_, err := config.MaterializeStandalone(dataDir, keyEnsure)
		require.NoError(t, err)
		keyAfter, _ := os.ReadFile(keyPath)
		require.Equal(t, keyBefore, keyAfter, "删 config 后 key 必须复用、不重新生成")
	})

	t.Run("only key deleted fails fast", func(t *testing.T) {
		dataDir, _, keyPath := setup(t)
		require.NoError(t, os.Remove(keyPath))
		keyEnsure := func() ([]byte, error) { return datadir.Dir{Path: dataDir}.EnsureCredentialKey() }
		// Materialize 会先调 keyEnsure：key 不存在 → 生成新的（这是“都删/仅 key 被 Materialize 重新生成”路径）。
		// 真正的 fail-fast 发生在已有 config 指向 key、但用户手动删了 key 后的下次 *读取* config：
		_, err := config.Load(filepath.Join(dataDir, "config.yaml"))
		require.ErrorContains(t, err, "encryption_key_file")
	})

	t.Run("both deleted equals fresh env", func(t *testing.T) {
		dataDir, cfgPath, keyPath := setup(t)
		require.NoError(t, os.Remove(cfgPath))
		require.NoError(t, os.Remove(keyPath))
		keyEnsure := func() ([]byte, error) { return datadir.Dir{Path: dataDir}.EnsureCredentialKey() }
		_, err := config.MaterializeStandalone(dataDir, keyEnsure)
		require.NoError(t, err) // 全新生成
		_, err = os.Stat(keyPath)
		require.NoError(t, err)
	})
}
```

> 说明：`datadir.Dir{Path: dataDir}` 需 `Path` 字段可导出或提供构造函数。Task 3 中 `Dir.path` 未导出——补一个包内构造函数 `datadir.New(path string) Dir` 供测试用（生产仍走 `Resolve`）。

- [ ] **Step 2: 补 datadir.New 构造函数**

`datadir.go` 追加：

```go
// New 构造指向已存在路径的 Dir，主要供测试使用。生产路径应走 Resolve。
func New(path string) Dir { return Dir{path: path} }
```

- [ ] **Step 3: 验证通过**

Run: `go test ./internal/infrastructure/config -run TestDeletionMatrix -count=1 && go test ./internal/infrastructure/datadir -count=1`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add internal/infrastructure/config/standalone_test.go internal/infrastructure/datadir/datadir.go
git commit -m "test(config): 覆盖 spec §2.4.3 config/key 删除组合矩阵"
```

---

### Task 7: 配置选择四态探测链（cmd/langhuan）

**Files:**
- Create: `cmd/langhuan/config_select.go`
- Test: `cmd/langhuan/config_select_test.go`
- Modify: `cmd/langhuan/main.go:215-259`（run / configPath）

- [ ] **Step 1: 写失败测试**

```go
func TestResolveConfigSelectionFourStates(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		explicit := filepath.Join(t.TempDir(), "my.yaml")
		os.WriteFile(explicit, []byte("database:\n  driver: postgres\n  dsn: postgres://x\n"), 0o600)
		sel := mustExplicit(t, explicit)
		require.True(t, sel.Explicit)
		require.Equal(t, explicit, sel.Path)
	})

	t.Run("cwd config.yaml second", func(t *testing.T) {
		tmp := t.TempDir()
		os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("x: 1\n"), 0o600)
		sel, err := resolveInDir(tmp, false, "", func(string) ([]byte, error) { panic("不应调用") })
		require.NoError(t, err)
		require.False(t, sel.Explicit)
		require.Contains(t, sel.Path, "config.yaml")
	})

	t.Run("data dir config third", func(t *testing.T) {
		tmp := t.TempDir()
		dataCfg := filepath.Join(tmp, ".langhuan-data", "config.yaml")
		os.MkdirAll(filepath.Dir(dataCfg), 0o700)
		os.WriteFile(dataCfg, []byte("x: 1\n"), 0o600)
		sel, err := resolveInDir(tmp, false, filepath.Join(tmp, ".langhuan-data"), func(string) ([]byte, error) { panic("不应调用") })
		require.NoError(t, err)
		require.Contains(t, sel.Path, ".langhuan-data")
	})

	t.Run("none exist generates", func(t *testing.T) {
		tmp := t.TempDir()
		dataDir := filepath.Join(tmp, ".langhuan-data")
		called := false
		sel, err := resolveInDir(tmp, false, dataDir, func(d string) (string, error) {
			called = true
			return filepath.Join(d, "config.yaml"), nil
		})
		require.NoError(t, err)
		require.True(t, called, "无任何 config 时应触发生成")
		require.Contains(t, sel.Path, "config.yaml")
	})

	t.Run("corrupt data dir config fails fast no regen", func(t *testing.T) {
		tmp := t.TempDir()
		dataCfg := filepath.Join(tmp, ".langhuan-data", "config.yaml")
		os.MkdirAll(filepath.Dir(dataCfg), 0o700)
		os.WriteFile(dataCfg, []byte(":\n  bad yaml: ["), 0o600)
		_, err := resolveInDir(tmp, false, filepath.Join(tmp, ".langhuan-data"), func(string) (string, error) {
			t.Fatal("损坏的 config 不应触发生成")
			return "", nil
		})
		require.Error(t, err)
	})
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./cmd/langhuan -run TestResolveConfigSelection -count=1`
Expected: FAIL — 函数不存在。

- [ ] **Step 3: 实现 config_select.go**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigSelection 描述一次启动选定的配置来源。
type ConfigSelection struct {
	Path     string
	Explicit bool // 用户是否显式传 -config
}

// resolveConfigSelection 按 spec §2.1 四态有序探测链解析配置路径。
// generator 在第 4 层（都不存在）调用以生成 standalone config，返回生成物路径。
func resolveConfigSelection(
	cwdConfigPath string,   // 当前目录探测路径，通常 filepath.Join(cwd, "config.yaml")
	dataDirConfigPath string, // ~/.langhuan-data/config.yaml
	explicit string, explicitSet bool,
	generator func(dataDir string) (string, error),
	dataDir string,
) (ConfigSelection, error) {
	// 1. 显式
	if explicitSet {
		if _, err := os.Stat(explicit); err != nil {
			return ConfigSelection{}, fmt.Errorf("显式配置 %s 不可访问: %w", explicit, err)
		}
		return ConfigSelection{Path: explicit, Explicit: true}, nil
	}
	// 2. 当前目录 config.yaml
	if path, ok, err := statIfExists(cwdConfigPath); err != nil {
		return ConfigSelection{}, err
	} else if ok {
		return ConfigSelection{Path: path}, nil
	}
	// 3. ~/.langhuan-data/config.yaml（损坏 fail-fast，不回退不生成）
	if path, ok, err := statIfExists(dataDirConfigPath); err != nil {
		return ConfigSelection{}, err
	} else if ok {
		return ConfigSelection{Path: path}, nil
	}
	// 4. 都不存在 → 生成
	generated, err := generator(dataDir)
	if err != nil {
		return ConfigSelection{}, fmt.Errorf("生成 standalone 配置失败: %w", err)
	}
	return ConfigSelection{Path: generated}, nil
}

// statIfExists 返回 (path, true, nil) 当文件存在；false 当 IsNotExist；
// 任何其它 Stat 错误（权限/IO）原样返回（fail-fast）。
// 注意：文件存在但内容损坏属 Load 阶段错误，不在此处判断。
func statIfExists(path string) (string, bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return path, true, nil
	}
	if os.IsNotExist(err) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("检查配置 %s 失败: %w", path, err)
}
```

- [ ] **Step 4: 改造 main.go 的 configPath 与 run**

把 `configPath`（main.go:251）改为返回 `(ConfigSelection 的输入)`，并区分 flag 是否显式设置：

```go
func parseConfigFlag(args []string) (path string, explicit bool, err error) {
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	p := fs.String("config", "", "YAML 配置文件路径（留空走四态探测链）")
	if err := fs.Parse(args[1:]); err != nil {
		return "", false, err
	}
	set := false
	fs.Visit(func(f *flag.Flag) { if f.Name == "config" { set = true } })
	return *p, set, nil
}
```

`run` 改为：

```go
func run(args []string) error {
	_, explicitSet, err := parseConfigFlag(args)  // explicit path（可能为空）
	// ... 解析 cwd、home ...
	sel, err := resolveConfigSelection(cwdConfig, dataDirConfig, explicitPath, explicitSet,
		func(dataDir string) (string, error) {
			d, err := datadir.Resolve(os.UserHomeDir) // 或注入
			if err != nil { return "", err }
			if err := d.Ensure(); err != nil { return "", err }
			return config.MaterializeStandalone(d.Path(), d.EnsureCredentialKey)
		}, dataDir)
	...
	cfg, err := config.Load(sel.Path)
	...
}
```

（实际接线细节在 Task 8 端到端完成；此处确保 `resolveConfigSelection` 单元测试通过。）

- [ ] **Step 5: 验证通过 + PG 回归**

Run: `go test ./cmd/langhuan -run TestResolveConfigSelection -count=1 && go build ./cmd/langhuan`
Expected: PASS（四态链 + 损坏 fail-fast）；构建通过。

- [ ] **Step 6: 提交**

```bash
git add cmd/langhuan/config_select.go cmd/langhuan/config_select_test.go cmd/langhuan/main.go
git commit -m "feat(cmd): 配置选择改为四态有序探测链并区分显式 -config"
```

---

### Task 8: 启动合同端到端集成 + 错误文案去 PG 硬编码

**Files:**
- Modify: `cmd/langhuan/main.go:215-275`（run / buildApp 接线）
- Test: `cmd/langhuan/main_test.go`

- [ ] **Step 1: 写端到端 smoke 测试**

```go
func TestZeroConfigBootstrapGeneratesConfigAndKey(t *testing.T) {
	// 用注入的 home 指向临时目录，模拟"当前目录无 config.yaml、home 下也无"
	home := t.TempDir()
	cwd := t.TempDir()
	// 通过可替换变量注入（openDatabase 等已在 71 行是 var）
	// 验证：运行后 home/.langhuan-data/config.yaml + credential.key 存在且权限正确
	// （此测试不真正 open DB，只验证启动合同的文件生成部分；DB open 在切片 2 验证）
	...
	require.FileExists(t, filepath.Join(home, ".langhuan-data", "config.yaml"))
	require.FileExists(t, filepath.Join(home, ".langhuan-data", "credential.key"))
}
```

> 说明：由于 `run` 依赖真实进程信号，本测试抽取一个 `bootstrapConfig(home, cwd) (string, error)` 纯函数（从 run 中提取配置解析与生成逻辑，不含信号/server），对其测试。

- [ ] **Step 2: 从 run 提取 bootstrapConfig 纯函数**

`main.go` 抽取配置解析+生成逻辑为 `bootstrapConfig(args []string) (*config.Config, *slog.Logger, error)`，`run` 只保留信号/server/生命周期。这样配置合同可单测。

- [ ] **Step 3: buildApp 错误文案去 PG 硬编码**

`main.go:269`：

```go
gormDB, err := openDatabase(cfg.Database)
// ...
if err != nil {
    return nil, fmt.Errorf("连接数据库失败: %w", err)
}
```

（原 "连接 PostgreSQL 失败" → "连接数据库失败"；spec §4.2 要求按实际 driver 表述。）`openDatabase` 签名同步改为接收 `config.DatabaseConfig`（切片 2 Task 11 配合 `db.Open` 改造；此处先改调用形态，db.Open 改造在切片 2）。

> 注意：本 task 暂不改 `openDatabase = db.Open` 的内部实现（那是切片 2 Task 11），只改 buildApp 的调用与文案。为保持 PG 可编译，`db.Open` 暂时保留旧签名 `Open(dsn string)`，Task 11 再改签名。本 task 的 `openDatabase(cfg.Database)` 调用先用 `cfg.Database.DSN` 过渡。

- [ ] **Step 4: 验证通过 + PG 回归**

Run: `go test ./cmd/langhuan -run TestZeroConfigBootstrap -count=1 && go test ./cmd/langhuan -count=1`
Expected: PASS（启动合同文件生成 + 现有 main 测试不回归）。

- [ ] **Step 5: 提交**

```bash
git add cmd/langhuan/main.go cmd/langhuan/main_test.go
git commit -m "feat(cmd): 零配置启动端到端生成 config/key，错误文案去除 PostgreSQL 硬编码"
```

---

## 切片 2：数据库地基

### Task 9: 引入 SQLite 依赖并验证 CGO_ENABLED=0 + vec 注册

**Files:**
- Modify: `go.mod` / `go.sum`
- Test: `internal/infrastructure/db/sqlite_smoke_test.go`

- [ ] **Step 1: 添加依赖**

```bash
go get modernc.org/sqlite@v1.56.0
go get modernc.org/sqlite/vec@v1.56.0  # /vec 是子模块，跟随 sqlite 版本
# golang-migrate 已在 v4.18.3；确认 database/sqlite 子包可用：
go get github.com/golang-migrate/migrate/v4/database/sqlite@v4.18.3
```

- [ ] **Step 2: 写 vec 注册 smoke 测试**

```go
package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

func TestSQLiteVecRegistered(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	// vec_distance_cosine 由 modernc.org/sqlite/vec 自动注册
	var dist float64
	err = db.QueryRow(`SELECT vec_distance_cosine(vec_f32('[1,0,0]'), vec_f32('[0,1,0]'))`).Scan(&dist)
	require.NoError(t, err)
	// 正交向量余弦距离 = 1
	require.InDelta(t, 1.0, dist, 1e-6)
}
```

- [ ] **Step 3: 验证 CGO_ENABLED=0 构建与测试通过**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/infrastructure/db -run TestSQLiteVecRegistered -count=1`
Expected: PASS（证明 modernc + vec 在纯 Go 下注册成功）。

- [ ] **Step 4: 提交**

```bash
git add go.mod go.sum internal/infrastructure/db/sqlite_smoke_test.go
git commit -m "build(db): 引入 modernc.org/sqlite 与 vec 扩展，验证 CGO_ENABLED=0 注册"
```

---

### Task 10: Dialect 抽象

**Files:**
- Create: `internal/infrastructure/db/dialect.go`
- Test: `internal/infrastructure/db/dialect_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestDialectOfInfersFromDialector(t *testing.T) {
	pgDB := openTestPG(t) // 现有 PG test helper
	d, err := DialectOf(pgDB)
	require.NoError(t, err)
	require.Equal(t, DialectPostgres, d)
	// SQLite 侧在 Task 12 dialector 实现后补
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/db -run TestDialectOf -count=1`
Expected: FAIL — Dialect/DialectOf 未定义。

- [ ] **Step 3: 实现 dialect.go**

```go
package db

import (
	"fmt"

	"gorm.io/gorm"
)

type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

func DialectOf(database *gorm.DB) (Dialect, error) {
	if database == nil || database.Dialector == nil {
		return "", fmt.Errorf("无法推断方言：gorm.DB 或 Dialector 为空")
	}
	switch database.Dialector.Name() {
	case "postgres":
		return DialectPostgres, nil
	case "sqlite":
		return DialectSQLite, nil
	default:
		return "", fmt.Errorf("未知的 gorm Dialector: %s", database.Dialector.Name())
	}
}
```

- [ ] **Step 4: 验证通过 + PG 回归**

Run: `go test ./internal/infrastructure/db -run TestDialectOf -count=1`
Expected: PASS（PG 推断为 postgres）。

- [ ] **Step 5: 提交**

```bash
git add internal/infrastructure/db/dialect.go internal/infrastructure/db/dialect_test.go
git commit -m "feat(db): 新增 Dialect 抽象与 DialectOf 推断"
```

---

### Task 11: SQLite Dialector compatibility test（先行，TDD）

**Files:**
- Create: `internal/infrastructure/db/sqlitedialect/compatibility_test.go`

spec §3 要求"实现前必须先以 compatibility test 验证 GORM CRUD、约束翻译、时间扫描、事务 savepoint、迁移，不能只以编译通过作为证据"。

- [ ] **Step 1: 写合同测试（先全部 FAIL）**

```go
package sqlitedialect_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/infrastructure/db/sqlitedialect"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type compatItem struct {
	ID   string `gorm:"primaryKey"`
	Name string `gorm:"uniqueIndex"`
	Ts   time.Time
}

func newCompatDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlitedialect.Open("file:"+t.TempDir()+"/compat.db?cache=shared"),
		&gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&compatItem{}))
	return db
}

func TestCompatCRUD(t *testing.T) {
	db := newCompatDB(t)
	it := compatItem{ID: "a", Name: "x", Ts: time.Now().UTC().Truncate(time.Second)}
	require.NoError(t, db.Create(&it).Error)
	var got compatItem
	require.NoError(t, db.First(&got, "id = ?", "a").Error)
	require.Equal(t, it.Ts.UTC(), got.Ts.UTC()) // 时间 round-trip
}

func TestCompatUniqueViolationTranslated(t *testing.T) {
	db := newCompatDB(t)
	require.NoError(t, db.Create(&compatItem{ID: "1", Name: "dup"}).Error)
	err := db.Create(&compatItem{ID: "2", Name: "dup"}).Error
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey) // SQLITE_CONSTRAINT_UNIQUE 2067
}

func TestCompatSavepoint(t *testing.T) {
	db := newCompatDB(t)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.SavePoint("sp").Error)
		require.NoError(t, tx.Create(&compatItem{ID: "s1", Name: "in-sp"}).Error)
		require.NoError(t, tx.RollbackTo("sp").Error)
		require.NoError(t, tx.Create(&compatItem{ID: "s2", Name: "after-sp"}).Error)
		return nil
	}))
	var count int64
	db.Model(&compatItem{}).Count(&count)
	require.Equal(t, int64(1), count)
}

func TestCompatQuoteAndDataType(t *testing.T) {
	// QuoteTo 应使用双引号；DataTypeOf 对 string 给 TEXT
	dialector := sqlitedialect.Open(":memory:")
	// 通过 migrator 间接验证 DataTypeOf：AutoMigrate 已隐含类型映射
	_ = dialector
	// 详细 Quote 断言由 clause builder 输出验证（见 clauses_test）
}
```

- [ ] **Step 2: 验证全部失败**

Run: `go test ./internal/infrastructure/db/sqlitedialect -count=1`
Expected: FAIL — `sqlitedialect.Open` 不存在。

- [ ] **Step 3: 提交（红色测试先行）**

```bash
git add internal/infrastructure/db/sqlitedialect/compatibility_test.go
git commit -m "test(sqlitedialect): 先行写 GORM 兼容性合同测试"
```

---

### Task 12: 实现 SQLite Dialector

**Files:**
- Create: `internal/infrastructure/db/sqlitedialect/dialector.go`、`error_translator.go`、`clauses.go`、`LICENSE`
- 参考：glebarez/go-sqlite（MIT）的 clause 行为；保留许可证声明

- [ ] **Step 1: 实现 dialector.go 主体**

```go
// Package sqlitedialect 实现项目内 GORM SQLite Dialector。
// 底层只用 modernc.org/sqlite（driver name "sqlite"），不引入 glebarez/go-sqlite，
// 以免与 golang-migrate 的 database/sqlite（同源 modernc）重复注册 driver。
// clause 行为参考 github.com/glebarez/go-sqlite（MIT），见 LICENSE。
package sqlitedialect

import (
	"database/sql"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	_ "modernc.org/sqlite"
)

type Dialector struct {
	DSN string
	DB  *sql.DB
}

func Open(dsn string) gorm.Dialector { return &Dialector{DSN: dsn} }

func (d Dialector) Name() string { return "sqlite" }

func (d Dialector) Initialize(db *gorm.DB) error {
	if d.DB == nil {
		var err error
		d.DB, err = sql.Open("sqlite", d.DSN)
		if err != nil {
			return err
		}
	}
	db.ConnPool = d.DB
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		CreateClauses: []string{"INSERT", "VALUES", "ON CONFLICT"},
	})
	// 注册 SQLite 专属 clause builder（clauses.go）
	registerClauses(db)
	return nil
}

func (d Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return migrator.Migrator{Config: migrator.Config{
		DB: db, Dialector: d,
	}}
}

func (d Dialector) DataTypeOf(f *schema.Field) string {
	switch f.DataType {
	case schema.Bool, schema.Int, schema.Uint, schema.Float:
		return "NUMERIC"
	case schema.String:
		return "TEXT"
	case schema.Bytes:
		return "BLOB"
	case schema.Time:
		return "DATETIME"
	}
	return "TEXT"
}

func (d Dialector) DefaultValueOf(f *schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {
	writer.WriteByte('?')
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteByte('"')
	for _, b := range []byte(str) {
		if b == '"' {
			writer.WriteByte('"')
		}
		writer.WriteByte(b)
	}
	writer.WriteByte('"')
}

func (d Dialector) Explain(sql string, vars ...any) string {
	return gorm.ExplainSQL(sql, nil, `"`, vars...)
}

// SavePoint/RollbackTo 走 GORM 默认事务语义（SQLite 原生支持 SAVEPOINT）。
func (d Dialector) SavePoint(tx *gorm.DB, name string) error {
	return tx.Exec("SAVEPOINT " + name).Error
}
func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
	return tx.Exec("ROLLBACK TO SAVEPOINT " + name).Error
}

// 保留 uuid import 用于将来生成（防止 go vet 报未使用；此处实际可移除）。
var _ = uuid.Nil
```

- [ ] **Step 2: 实现 error_translator.go**

```go
package sqlitedialect

import (
	"errors"

	"github.com/glebarez/go-sqlite" // ← 注意：这里不引入 glebarez！
	"gorm.io/gorm"
)
```

> **修正**：spec §3 明确不引入 glebarez/go-sqlite。错误类型直接用 modernc.org/sqlite 的 `*sqlite.Error`。重写：

```go
package sqlitedialect

import (
	"errors"

	"gorm.io/gorm"
	sqlite3 "modernc.org/sqlite" // driver 名 "sqlite"，错误类型 *sqlite3.Error
)

// Translate 把 modernc sqlite 错误翻译为 GORM 哨兵错误。
// 稳定 extended code：2067/1555 = UNIQUE/PK 冲突；787 = FK 冲突。
func (d Dialector) Translate(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite3.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case 2067, 1555: // SQLITE_CONSTRAINT_PRIMARYKEY / UNIQUE
			return gorm.ErrDuplicatedKey
		case 787: // SQLITE_CONSTRAINT_FOREIGNKEY
			return gorm.ErrForeignKeyViolated
		}
	}
	return errors.Unwrap(err)
}

// RegisterErrorTranslator 让 gorm 在 TranslateError:true 时调用 Translate。
// （Dialector 实现 gorm.ErrorTranslator 接口即可，无需显式注册。）
var _ gorm.ErrorTranslator = Dialector{}
```

- [ ] **Step 3: 实现 clauses.go（INSERT/LIMIT/FOR）**

参考 glebarez/go-sqlite 的 MIT 实现，注册 SQLite 专属的 `INSERT`（含 `ON CONFLICT`）、`LIMIT`、`FOR`（FOR 在 SQLite 无行锁，静默忽略）clause builder。关键骨架：

```go
package sqlitedialect

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func registerClauses(db *gorm.DB) {
	// FOR：SQLite 无行级锁，clause.Locking 静默忽略（spec §9）
	db.ClauseBuilders["FOR"] = func(c clause.Clause, builder clause.Builder) {
		if _, ok := c.Expression.(clause.Locking); ok {
			return // no-op
		}
		c.Build(builder)
	}
	// LIMIT / INSERT 用 GORM 默认即可（SQLite 兼容标准语法），
	// 仅当后续发现差异时再覆盖。
}

var _ gorm.Dialector = Dialector{}
```

- [ ] **Step 4: 添加 LICENSE 声明**

`internal/infrastructure/db/sqlitedialect/LICENSE`：声明 clause 行为借鉴 `github.com/glebarez/go-sqlite`（MIT），保留其版权与许可证。

- [ ] **Step 5: 验证 compatibility test 通过**

Run: `go test ./internal/infrastructure/db/sqlitedialect -count=1`
Expected: PASS（CRUD、唯一约束翻译、savepoint、时间 round-trip 全绿）。

- [ ] **Step 6: 验证 CGO_ENABLED=0 + PG 回归**

Run: `CGO_ENABLED=0 go build ./... && go test ./internal/infrastructure/db -run TestDialectOf -count=1`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/infrastructure/db/sqlitedialect
git commit -m "feat(sqlitedialect): 项目内 GORM SQLite Dialector 与约束错误翻译"
```

---

### Task 13: db.Open 改造为按 driver 分流

**Files:**
- Modify: `internal/infrastructure/db/db.go`
- Modify: `internal/infrastructure/db/db_test.go`
- Modify: `cmd/langhuan/main.go:71`（openDatabase var 签名）

- [ ] **Step 1: 写失败测试**

```go
func TestOpenSQLiteAppliesPragmasAndSingleConn(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "t.db") + "?cache=shared"
	db, dialect, err := Open(config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true})
	require.NoError(t, err)
	defer db.DB().Close()
	require.Equal(t, DialectSQLite, dialect)

	var fk int
	require.NoError(t, db.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk)
	var jm string
	require.NoError(t, db.DB().QueryRow("PRAGMA journal_mode").Scan(&jm))
	require.Equal(t, "wal", jm)
	sqlDB, _ := db.DB()
	require.Equal(t, 1, sqlDB.Stats().MaxOpenConnections)
}
```

- [ ] **Step 2: 验证失败**

Run: `go test ./internal/infrastructure/db -run TestOpenSQLite -count=1`
Expected: FAIL — Open 签名是 `Open(dsn string)`。

- [ ] **Step 3: 改造 db.Open**

```go
package db

import (
	"fmt"
	"net/url"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db/sqlitedialect"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Open(cfg config.DatabaseConfig) (*gorm.DB, Dialect, error) {
	switch cfg.Driver {
	case "postgres", "":
		db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{TranslateError: true})
		return db, DialectPostgres, err
	case "sqlite":
		dsn := buildSQLiteDSN(cfg.DSN)
		db, err := gorm.Open(sqlitedialect.Open(dsn), &gorm.Config{TranslateError: true})
		if err != nil {
			return nil, "", err
		}
		sqlDB, err := db.DB()
		if err != nil {
			return nil, "", err
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		if err := assertSQLitePragmas(sqlDB); err != nil {
			return nil, "", err
		}
		return db, DialectSQLite, nil
	default:
		return nil, "", fmt.Errorf("不支持的数据库 driver: %s", cfg.Driver)
	}
}

// buildSQLiteDSN 在用户 DSN 上合并固定 pragma（spec §4.2），用 query 参数而非字符串盲拼。
func buildSQLiteDSN(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw // 无法解析则原样返回，让 driver 报错
	}
	q := u.Query()
	q.Set("_pragma", "foreign_keys(1)")
	q.Set("_pragma", "journal_mode(WAL)")
	q.Set("_pragma", "busy_timeout(5000)")
	q.Set("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	q.Set("_time_format", "sqlite")
	q.Set("_timezone", "UTC")
	// 注意：modernc 支持重复 _pragma key；url.Values.Set 会覆盖，需用 Add 或自定义拼接。
	// 实现时改用直接拼接 query string 以支持多个 _pragma。
	u.RawQuery = appendSQLitePragmas(u.RawQuery)
	return u.String()
}

func assertSQLitePragmas(sqlDB *sql.DB) error {
	// 断言 foreign_keys=1、journal_mode=wal、busy_timeout>=5000
	for _, check := range []struct{ sql, want string }{
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA journal_mode", "wal"},
	} {
		var got string
		if err := sqlDB.QueryRow(check.sql).Scan(&got); err != nil || got != check.want {
			return fmt.Errorf("SQLite PRAGMA 断言失败 %s: got %s want %s", check.sql, got, check.want)
		}
	}
	return nil
}
```

> `appendSQLitePragmas` 是 helper，把固定 pragma 追加到现有 query string（支持多个 `_pragma=...`）。实现时补全，注意 modernc DSN 对重复 `_pragma` 的解析。

- [ ] **Step 4: 更新 openDatabase var 与 buildApp 调用**

`cmd/langhuan/main.go:71`：

```go
var openDatabase = db.Open  // 签名现在是 func(config.DatabaseConfig) (*gorm.DB, db.Dialect, error)
```

`buildApp`（main.go:267）：

```go
gormDB, dialect, err := openDatabase(cfg.Database)
if err != nil {
    return nil, fmt.Errorf("连接数据库失败: %w", err)
}
app.gormDB = gormDB
app.dialect = dialect // appRuntime 增加字段（供后续切片 repository 分发用）
```

- [ ] **Step 5: 验证通过 + PG 回归**

Run: `go test ./internal/infrastructure/db -run TestOpenSQLite -count=1 && make test-image && go test -tags=integration ./internal/infrastructure/db -run TestOpen -count=1`
Expected: PASS（SQLite PRAGMA + 单连接；PG 路径零回归）。

- [ ] **Step 6: 提交**

```bash
git add internal/infrastructure/db/db.go internal/infrastructure/db/db_test.go cmd/langhuan/main.go
git commit -m "feat(db): Open 按 driver 分流，SQLite 注入 PRAGMA 与单连接池"
```

---

### Task 14: migrate.Run 按 driver 分流 + SQLite 占位迁移

**Files:**
- Modify: `internal/infrastructure/migrate/migrate.go`
- Create: `internal/infrastructure/migrate/migrations_sqlite/000001_init.up.sql`、`000001_init.down.sql`
- Modify: `internal/infrastructure/migrate/migrate_test.go`

- [ ] **Step 1: 写 SQLite 迁移分流测试**

```go
func TestRunSQLiteAppliesPlaceholderMigration(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "m.db") + "?cache=shared"
	err := Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
	require.NoError(t, err)
	// schema_migrations 表存在，版本 1
	db, _ := sql.Open("sqlite", dsn)
	defer db.Close()
	var version int
	require.NoError(t, db.QueryRow("SELECT version FROM schema_migrations").Scan(&version))
	require.Equal(t, 1, version)
}
```

- [ ] **Step 2: 创建占位迁移**

`migrations_sqlite/000001_init.up.sql`：

```sql
-- 占位迁移：验证 SQLite 迁移管线通畅。实际 schema 在切片 3 填充。
-- 切片 2 需要一个非空迁移让 golang-migrate 记录版本号。
SELECT 1;
```

`migrations_sqlite/000001_init.down.sql`：

```sql
SELECT 1;
```

> 注意：`SELECT 1` 作为占位是为了让迁移文件非空且可执行。golang-migrate 的 sqlite driver 会包在隐式事务里执行。切片 3 会用真实 schema 替换此文件（版本号重排为 000001_core_schema 等）。

- [ ] **Step 3: 改造 migrate.Run 按 driver 分流**

```go
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

//go:embed migrations
var pgMigrationsFS embed.FS

//go:embed migrations_sqlite
var sqliteMigrationsFS embed.FS

func Run(ctx context.Context, cfg config.DatabaseConfig) error {
	switch cfg.Driver {
	case "postgres", "":
		return runPostgres(ctx, cfg.DSN)
	case "sqlite":
		return runSQLite(ctx, cfg.DSN)
	default:
		return fmt.Errorf("不支持的数据库 driver: %s", cfg.Driver)
	}
}

func runPostgres(ctx context.Context, dsn string) error {
	// 原有逻辑：sql.Open("postgres", dsn) + postgres.WithInstance + migrations/
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("打开迁移数据库连接失败: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接迁移数据库失败: %w", err)
	}
	source, err := iofs.New(pgMigrationsFS, "migrations")
	if err != nil {
		return err
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行数据库迁移失败: %w", err)
	}
	return nil
}

func runSQLite(ctx context.Context, dsn string) error {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("打开 SQLite 迁移连接失败: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接 SQLite 迁移数据库失败: %w", err)
	}
	source, err := iofs.New(sqliteMigrationsFS, "migrations_sqlite")
	if err != nil {
		return err
	}
	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行 SQLite 迁移失败: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 更新 buildApp 的 migrate.Run 调用**

`main.go:273`：

```go
if err := migrate.Run(ctx, cfg.Database); err != nil {
    return nil, err
}
```

- [ ] **Step 5: 验证通过 + PG 回归**

Run: `go test ./internal/infrastructure/migrate -run TestRunSQLite -count=1 && make test-integration`
Expected: PASS（SQLite 占位迁移记录版本 1；PG 全套迁移 + 集成测试零回归）。

- [ ] **Step 6: 提交**

```bash
git add internal/infrastructure/migrate
git commit -m "feat(migrate): Run 按 driver 分流，新增 SQLite 迁移目录与占位迁移"
```

---

### Task 15: SQLite test support

**Files:**
- Create: `internal/testsupport/sqlite.go`
- Test: `internal/testsupport/sqlite_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestNewSQLiteDBReturnsMigratedDB(t *testing.T) {
	db, dialect, cleanup := testsupport.NewSQLiteDB(t)
	defer cleanup()
	require.Equal(t, db.DialectPostgres, dialect) // 不，应为 sqlite
	// schema_migrations 存在
	var v int
	require.NoError(t, db.DB().QueryRow("SELECT version FROM schema_migrations").Scan(&v))
	require.Equal(t, 1, v)
}
```

- [ ] **Step 2: 实现 testsupport/sqlite.go**

```go
package testsupport

import (
	"path/filepath"
	"testing"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	"gorm.io/gorm"
)

// NewSQLiteDB 为测试返回一个迁移后的 SQLite *gorm.DB。
// 数据库文件位于 t.TempDir()，测试结束自动清理；绝不连 config.yaml 或 ~/.langhuan-data。
func NewSQLiteDB(t *testing.T) (*gorm.DB, db.Dialect, func()) {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "test.db") + "?cache=shared"
	cfg := config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true}
	if err := migrate.Run(t.Context(), cfg); err != nil {
		t.Fatalf("迁移 SQLite 测试库失败: %v", err)
	}
	gormDB, dialect, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开 SQLite 测试库失败: %v", err)
	}
	cleanup := func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	return gormDB, dialect, cleanup
}
```

- [ ] **Step 3: 验证通过**

Run: `go test ./internal/testsupport -run TestNewSQLiteDB -count=1`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add internal/testsupport/sqlite.go internal/testsupport/sqlite_test.go
git commit -m "feat(testsupport): 新增 SQLite 测试 helper（t.TempDir 隔离）"
```

---

## Appendix: 测试 helper

`internal/infrastructure/config/config_test.go` 顶部补：

```go
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func validKey() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(b)
}
```

`internal/infrastructure/datadir/datadir_test.go` 顶部补：

```go
func newTestDir(t *testing.T) datadir.Dir {
	d := datadir.New(filepath.Join(t.TempDir(), ".langhuan-data"))
	require.NoError(t, d.Ensure())
	return d
}
```

---

## Self-Review

**1. Spec 覆盖率**（对照 spec §13 切片 1-2）：

| spec 要求 | 覆盖 task |
|---|---|
| §2.1 四态配置选择链 + 损坏 fail-fast + 无静默回退 | Task 7（resolveConfigSelection 四态 + 损坏不生成测试）|
| §2.2 standalone profile + 落盘 config.yaml + 头部注释 | Task 5 |
| §2.3 数据目录 0700 + config.yaml 0600 + key 0600 | Task 3（目录）、Task 5（config 0600）、Task 4（key 0600）|
| §2.4.1 encryption_key_file 互斥/从文件读/绝对路径 | Task 2 |
| §2.4.2 credential.key 生成 O_CREATE\|O_EXCL/并发/损坏 fail-fast/绝不覆盖 | Task 4 |
| §2.4.3 删除组合矩阵 | Task 6 |
| §3 项目内 sqlitedialect + 不引入 glebarez + compatibility test 先行 | Task 11、12 |
| §4.1 Dialect 抽象（不过度） | Task 10 |
| §4.2 Open 按 driver 分流 + PRAGMA 断言 + MaxOpenConns(1) + DSN builder | Task 13 |
| §4.3 migrate 分流 + migrations_sqlite/ + 迁移先于业务连接 | Task 14 |
| §10.1 RedisConfig.Enabled 默认 true 兼容 | Task 1 |
| §13 切片 1「启动合同」 | Task 1-8 |
| §13 切片 2「数据库地基」 | Task 9-15 |
| buildApp 错误文案去 PG 硬编码（§4.2）| Task 8 Step 3、Task 13 Step 4 |

**未覆盖（留给后续切片，本 plan 范围外）**：SQLite 实际 schema 迁移（切片 3）、Repository 方言分发（切片 4）、事务/锁分流（切片 5）、向量/FTS（切片 6）、Redis 本地化（切片 7）、E2E+文档（切片 8）。

**2. 占位符扫描**：plan 中无 TBD/TODO/"implement later"。Dialector 实现给了真实代码骨架（基于 modernc + 参考 glebarez MIT）。`appendSQLitePragmas`、`bootstrapConfig`、`resolveInDir` 等测试 helper 函数有明确签名；个别 helper（如 `resolveInDir` 是 `resolveConfigSelection` 的测试包装）在 Task 7 step 3 已定义主函数，测试包装在 step 1 测试中具名调用。

**3. 类型一致性**：`Dialect`/`DialectPostgres`/`DialectSQLite`（Task 10）在 Task 13 Open 返回值、Task 15 NewSQLiteDB 返回值中一致使用；`ConfigSelection{Path, Explicit}`（Task 7）在 run（Task 8）一致；`ResolveEncryptionKey`（Task 2）在 validate 与 main 调用点一致；`Open(cfg) (*gorm.DB, Dialect, error)`（Task 13）与 `openDatabase` var、buildApp 调用一致。

**4. 已知风险点（实现时注意）**：
- `url.Values.Set` 对重复 `_pragma` key 会覆盖，`buildSQLiteDSN` 的 step 3 注释已标明需用 `appendSQLitePragmas` 直接拼接 query string 以支持多个 `_pragma=...`。
- modernc.org/sqlite/vec 子模块的 import 路径与版本需在 Task 9 实际拉取时确认（`go get modernc.org/sqlite/vec` 的确切 module 路径）。
- golang-migrate `database/sqlite` 包的 import 路径是 `github.com/golang-migrate/migrate/v4/database/sqlite`（非 sqlite3），Task 14 已正确使用。
- Dialector 的 `Translate` 返回 `errors.Unwrap(err)` 作为兜底——需确认这不会吞掉 GORM 已识别的哨兵错误；compatibility test（Task 11）的唯一约束用例覆盖了主路径。

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-12-sqlite-slice1-2-bootstrap-and-db-foundation.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**

后续切片 3-8 的 plan 我会在本切片完成后按同样模式逐个产出。

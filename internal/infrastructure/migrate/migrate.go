package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// migrationsFS 嵌入 PostgreSQL 迁移脚本。
//
//go:embed migrations
var migrationsFS embed.FS

// sqliteMigrationsFS 嵌入 SQLite 迁移脚本。SQLite schema 从 PG 最终语义出发，
// 不回放 PG 的历史重建与数据回填（spec §4.3）。
//
//go:embed migrations_sqlite
var sqliteMigrationsFS embed.FS

// Run 基于 cfg.Driver 建立迁移专用连接并执行向上迁移。
// 迁移完成后立即关闭该连接，不接管也不影响调用方的数据库连接。
// 当目标库已处于最新版本（无新迁移可执行）时返回 nil，不视为错误。
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

// RunDown 基于 cfg.Driver 建立迁移专用连接并执行向下回滚到版本 0。
// 迁移完成后立即关闭该连接。当目标库已处于版本 0（无迁移可回滚）时返回 nil，不视为错误。
// 主要供测试与回滚演练使用；生产环境回滚需谨慎评估数据丢失风险。
func RunDown(ctx context.Context, cfg config.DatabaseConfig) error {
	switch cfg.Driver {
	case "postgres", "":
		return runPostgresDown(ctx, cfg.DSN)
	case "sqlite":
		return runSQLiteDown(ctx, cfg.DSN)
	default:
		return fmt.Errorf("不支持的数据库 driver: %s", cfg.Driver)
	}
}

func runPostgres(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("打开迁移数据库连接失败: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接迁移数据库失败: %w", err)
	}

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("加载迁移脚本失败: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移 driver 失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("创建迁移实例失败: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行数据库迁移失败: %w", err)
	}
	return nil
}

func runSQLite(ctx context.Context, dsn string) error {
	db, err := sql.Open("sqlite", sqlitePragmaDSN(dsn, true))
	if err != nil {
		return fmt.Errorf("打开 SQLite 迁移连接失败: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接 SQLite 迁移数据库失败: %w", err)
	}

	source, err := iofs.New(sqliteMigrationsFS, "migrations_sqlite")
	if err != nil {
		return fmt.Errorf("加载 SQLite 迁移脚本失败: %w", err)
	}

	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("初始化 SQLite 迁移 driver 失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("创建 SQLite 迁移实例失败: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行 SQLite 迁移失败: %w", err)
	}
	return nil
}

// sqlitePragmaDSN 在用户 DSN 上追加与 db.buildSQLiteDSN 一致的 PRAGMA 与事务锁模式。
// migrate 包不能 import db 包（会循环依赖），故在此复制一份等价 helper。
// UP 路径启用 foreign_keys：迁移产生的表关系在后续业务连接的 FK 检查下必须成立。
// DOWN 路径关闭 foreign_keys：DROP TABLE IF EXISTS 的删除顺序可能不完全满足 FK 依赖，
// 关闭后允许以任意顺序回收表（teardown 标准做法）。
// modernc 支持 ?_pragma=key(val) 形式，多个 _pragma 用 & 连接。
func sqlitePragmaDSN(raw string, foreignKeys bool) string {
	fk := "0"
	if foreignKeys {
		fk = "1"
	}
	extra := "_pragma=foreign_keys(" + fk + ")" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + extra
}

func runSQLiteDown(ctx context.Context, dsn string) error {
	db, err := sql.Open("sqlite", sqlitePragmaDSN(dsn, false))
	if err != nil {
		return fmt.Errorf("打开 SQLite 迁移连接失败: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接 SQLite 迁移数据库失败: %w", err)
	}

	source, err := iofs.New(sqliteMigrationsFS, "migrations_sqlite")
	if err != nil {
		return fmt.Errorf("加载 SQLite 迁移脚本失败: %w", err)
	}

	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("初始化 SQLite 迁移 driver 失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("创建 SQLite 迁移实例失败: %w", err)
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("回滚 SQLite 迁移失败: %w", err)
	}
	return nil
}

func runPostgresDown(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("打开迁移数据库连接失败: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接迁移数据库失败: %w", err)
	}

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("加载迁移脚本失败: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移 driver 失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("创建迁移实例失败: %w", err)
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("回滚数据库迁移失败: %w", err)
	}
	return nil
}

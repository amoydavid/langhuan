package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db/sqlitedialect"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open 按 cfg.Driver 打开对应方言的 GORM 连接，返回数据库与已解析方言。
//   - postgres：沿用现有 postgres.Open 与默认连接池行为（零回归）
//   - sqlite：用项目内 sqlitedialect，注入固定 PRAGMA，单连接串行写
//
// SQLite DSN 在用户值上合并 _pragma（foreign_keys/journal_mode/busy_timeout/synchronous）
// 与 _txlock=immediate，确保所有连接行为一致。
func Open(cfg config.DatabaseConfig) (*gorm.DB, Dialect, error) {
	switch cfg.Driver {
	case "postgres", "":
		gormDB, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{TranslateError: true})
		return gormDB, DialectPostgres, err
	case "sqlite":
		gormDB, err := gorm.Open(sqlitedialect.Open(buildSQLiteDSN(cfg.DSN)), &gorm.Config{TranslateError: true})
		if err != nil {
			return nil, "", err
		}
		sqlDB, err := gormDB.DB()
		if err != nil {
			return nil, "", err
		}
		// 单写锁：串行所有写事务，避免 SQLITE_BUSY（单机场景足够）
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		if err := assertSQLitePragmas(sqlDB); err != nil {
			return nil, "", err
		}
		return gormDB, DialectSQLite, nil
	default:
		return nil, "", fmt.Errorf("不支持的数据库 driver: %s", cfg.Driver)
	}
}

// buildSQLiteDSN 在用户 DSN 上追加固定 pragma 与事务锁模式。
// modernc 支持 ?_pragma=key(val) 形式，多个 _pragma 用 & 连接。
func buildSQLiteDSN(raw string) string {
	// 注意：不设 _time_format/_timezone。modernc 默认能把时间列 Scan 为 time.Time
	//（compatibility test 验证 round-trip）；设 _time_format=sqlite 反而让 driver
	// 返回字符串，破坏 GORM 的 *time.Time 扫描。
	extra := "_pragma=foreign_keys(1)" +
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

// assertSQLitePragmas 在连接建立后断言关键 PRAGMA 生效，避免 DSN pragma 被静默忽略。
func assertSQLitePragmas(sqlDB *sql.DB) error {
	for _, check := range []struct{ sql, want string }{
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA journal_mode", "wal"},
	} {
		var got string
		if err := sqlDB.QueryRow(check.sql).Scan(&got); err != nil {
			return fmt.Errorf("SQLite PRAGMA 查询失败 %s: %w", check.sql, err)
		}
		if !strings.EqualFold(got, check.want) {
			return fmt.Errorf("SQLite PRAGMA 断言失败 %s: got %s want %s", check.sql, got, check.want)
		}
	}
	return nil
}

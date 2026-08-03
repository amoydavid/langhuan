package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

// migrationsFS 嵌入 migrations 目录下的 SQL 文件，使二进制自带迁移脚本，
// 运行时不依赖外部文件路径。
//
//go:embed migrations
var migrationsFS embed.FS

// Run 基于 databaseURL 建立一个迁移专用的数据库连接并执行向上迁移。
// 迁移完成后立即关闭该连接，不接管也不影响调用方的数据库连接。
// 当目标库已处于最新版本（无新迁移可执行）时返回 nil，不视为错误。
func Run(ctx context.Context, databaseURL string) error {
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
	// m.Close() 会关闭迁移 driver，进而关闭上面新建的迁移专用连接。
	// 关闭错误无业务影响，忽略即可。
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行数据库迁移失败: %w", err)
	}
	return nil
}

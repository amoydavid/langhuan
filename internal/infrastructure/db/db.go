package db

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Open(dsn string) (*gorm.DB, error) {
	// TranslateError 让 GORM 把底层 driver 的约束错误翻译成统一哨兵错误
	// （如 gorm.ErrDuplicatedKey / gorm.ErrForeignKeyViolated），
	// 这样 repository 层只需依赖 GORM 的哨兵，无需感知 pgx/pgconn/libpq 差异。
	return gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
}

package sqlitedialect

import (
	"errors"

	"gorm.io/gorm"
	sqlite3 "modernc.org/sqlite"
)

// Translate 把 modernc.org/sqlite 的驱动错误翻译为 GORM 哨兵错误，
// 使 repository 层只需依赖 gorm.ErrDuplicatedKey / gorm.ErrForeignKeyViolated，
// 无需感知底层 driver 差异。
//
// 稳定 extended code：
//   - 2067 SQLITE_CONSTRAINT_PRIMARYKEY
//   - 1555 SQLITE_CONSTRAINT_UNIQUE
//   - 787 SQLITE_CONSTRAINT_FOREIGNKEY
func (d Dialector) Translate(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite3.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case 2067, 1555:
			return gorm.ErrDuplicatedKey
		case 787:
			return gorm.ErrForeignKeyViolated
		}
	}
	// 非 sqlite driver 错误（包括 GORM 自身哨兵如 ErrRecordNotFound）必须原样返回，
	// 不能 errors.Unwrap——否则会把无 Unwrap 链的哨兵错误 unwrap 成 nil，使
	// RaiseErrorOnNotFound / 约束翻译等机制失效。
	return err
}

// 编译期断言：Dialector 实现 gorm.ErrorTranslator。
var _ gorm.ErrorTranslator = Dialector{}

package sqlitedialect

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// registerSQLiteClauses 注册 SQLite 专属的 GORM clause builder。
//
// 当前唯一需要覆盖的是 FOR clause：SQLite 没有行级锁（SELECT ... FOR UPDATE 不存在），
// 而 GORM 的 clause.Locking 在标准 SQLite 驱动下会原样生成 FOR UPDATE 导致语法错误。
// 这里把 Locking 静默降级为 no-op（spec §9：写事务已由 _txlock=immediate 串行化）。
//
// INSERT/LIMIT/VALUES/ON CONFLICT 用 GORM 默认 builder（SQLite 兼容标准语法）。
func registerSQLiteClauses(db *gorm.DB) {
	db.ClauseBuilders["FOR"] = func(c clause.Clause, builder clause.Builder) {
		if _, ok := c.Expression.(clause.Locking); ok {
			return // SQLite 无行锁，静默忽略
		}
		c.Build(builder)
	}
}

// 编译期断言：Dialector 实现 gorm.Dialector（由 dialector.go 的方法集合满足）。
var _ gorm.Dialector = Dialector{}

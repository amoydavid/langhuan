package db

import (
	"fmt"

	"gorm.io/gorm"
)

// Dialect 标识底层使用的 SQL 方言。Repository 在存在真实 SQL 差异处持有它做分支决策，
// 没有 SQL 差异的 Repository 不引入该字段（spec §4.1：最小方言抽象，不建大而全 capability framework）。
type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

// DialectOf 从已打开的 gorm.DB 推断方言。
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

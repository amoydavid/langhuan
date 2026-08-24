// Package sqlitedialect 实现项目内 GORM SQLite Dialector。
//
// 底层只用 modernc.org/sqlite（database/sql driver name "sqlite"），不引入
// github.com/glebarez/sqlite。原因是 glebarez 经 github.com/glebarez/go-sqlite
// 也注册名为 "sqlite" 的 driver，与 modernc.org/sqlite（本包与 golang-migrate
// database/sqlite 共用）重复注册会在 init 阶段 panic。
//
// 本 Dialector 只实现项目使用的 GORM 能力：CRUD、约束错误翻译、时间扫描、
// 事务 savepoint。SQLite schema 管理走 golang-migrate（migrations_sqlite/），
// 不依赖 GORM Migrator 的列变更能力。
//
// clause 行为参考 github.com/glebarez/go-sqlite 与 gorm.io/driver/sqlite（均 MIT），
// 见同目录 LICENSE。
package sqlitedialect

import (
	"database/sql"
	"fmt"
	"regexp"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	_ "modernc.org/sqlite"     // 注册 database/sql driver "sqlite"
	_ "modernc.org/sqlite/vec" // 注册 sqlite-vec 函数（vec_f32/vec_distance_cosine），standalone 向量检索依赖
)

// savePointNamePattern 限定 savepoint 名称字符集（GORM 生成 "sp"+数字，合法）。
// name 直接拼入 SAVEPOINT SQL，必须白名单校验以防注入。
var savePointNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Dialector 是基于 modernc.org/sqlite 的 GORM SQLite 方言。
type Dialector struct {
	DSN string
	DB  *sql.DB
}

// Open 返回一个使用给定 DSN 的 GORM Dialector。
func Open(dsn string) gorm.Dialector { return &Dialector{DSN: dsn} }

// Name 返回方言名，供 db.DialectOf 推断。
func (d Dialector) Name() string { return "sqlite" }

// Initialize 打开底层连接并注册 GORM callbacks 与 SQLite 专属 clause builder。
func (d Dialector) Initialize(db *gorm.DB) error {
	if d.DB == nil {
		opened, err := sql.Open("sqlite", d.DSN)
		if err != nil {
			return err
		}
		d.DB = opened
	}
	db.ConnPool = d.DB

	// SQLite 3.35+（modernc 1.56 内嵌 3.53）支持 RETURNING。BuildClauses 必须包含
	// FROM（DELETE/UPDATE 的表名来自 FROM clause）与 RETURNING（走 QueryContext 回查，
	// 避免部分路径下 ExecContext 返回 nil result）。配置参考 glebarez/sqlite（MIT）。
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		LastInsertIDReversed: true,
		CreateClauses:        []string{"INSERT", "VALUES", "ON CONFLICT", "RETURNING"},
		UpdateClauses:        []string{"UPDATE", "SET", "FROM", "WHERE", "RETURNING"},
		DeleteClauses:        []string{"DELETE", "FROM", "WHERE", "RETURNING"},
	})
	registerSQLiteClauses(db)
	return nil
}

// Migrator 返回 GORM 默认 migrator。SQLite 的 schema 由 golang-migrate 管理，
// 此处不重写 SQLite 专属列变更逻辑（生产路径不调 AutoMigrate）。
func (d Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return migrator.Migrator{Config: migrator.Config{DB: db, Dialector: d}}
}

// DataTypeOf 把 GORM 逻辑类型映射到 SQLite 亲和类型。
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

// DefaultValueOf 返回列默认值占位。
func (d Dialector) DefaultValueOf(f *schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

// BindVarTo 写出 SQLite 的位置参数占位符 ?。
func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {
	writer.WriteByte('?')
}

// QuoteTo 用双引号引用标识符，内部双引号转义为两个双引号。
func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteByte('"')
	for i := 0; i < len(str); i++ {
		if str[i] == '"' {
			writer.WriteByte('"')
		}
		writer.WriteByte(str[i])
	}
	writer.WriteByte('"')
}

// Explain 生成可读 SQL（用于日志）。
func (d Dialector) Explain(sql string, vars ...any) string {
	return logger.ExplainSQL(sql, nil, `"`, vars...)
}

// SavePoint 在事务内创建保存点。SQLite 原生支持 SAVEPOINT 语法。
// name 直接拼入 SQL，必须匹配白名单（GORM 生成 "sp"+数字），非法 name 返回 error。
func (d Dialector) SavePoint(tx *gorm.DB, name string) error {
	if !savePointNamePattern.MatchString(name) {
		return fmt.Errorf("非法 savepoint 名称: %q", name)
	}
	return tx.Exec("SAVEPOINT " + name).Error
}

// RollbackTo 回滚到命名保存点。name 同样需通过白名单校验。
func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
	if !savePointNamePattern.MatchString(name) {
		return fmt.Errorf("非法 savepoint 名称: %q", name)
	}
	return tx.Exec("ROLLBACK TO SAVEPOINT " + name).Error
}

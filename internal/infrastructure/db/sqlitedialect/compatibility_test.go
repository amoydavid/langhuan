package sqlitedialect_test

import (
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
	db, err := gorm.Open(sqlitedialect.Open("file:"+t.TempDir()+"/compat.db?cache=shared&_pragma=foreign_keys(1)"),
		&gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 用 raw SQL 建表，匹配生产 golang-migrate 路径（spec §3：SQLite schema 走
	// golang-migrate，不重写 GORM Migrator 的列变更/内联索引逻辑）。
	// GORM 默认 Migrator 会把 uniqueIndex 内联进 CREATE TABLE，SQLite 不支持该语法。
	require.NoError(t, db.Exec(`CREATE TABLE compat_items (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, ts DATETIME NOT NULL)`).Error)
	return db
}

// TestCompatCRUDAndTimeRoundTrip 验证基本 CRUD 与时间列 round-trip。
func TestCompatCRUDAndTimeRoundTrip(t *testing.T) {
	db := newCompatDB(t)
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)
	require.NoError(t, db.Create(&compatItem{ID: "a", Name: "x", Ts: now}).Error)

	var got compatItem
	require.NoError(t, db.First(&got, "id = ?", "a").Error)
	require.Equal(t, "a", got.ID)
	require.Equal(t, now.UTC(), got.Ts.UTC())
}

// TestCompatUniqueViolationTranslated 验证 SQLITE_CONSTRAINT_UNIQUE → gorm.ErrDuplicatedKey。
func TestCompatUniqueViolationTranslated(t *testing.T) {
	db := newCompatDB(t)
	require.NoError(t, db.Create(&compatItem{ID: "1", Name: "dup"}).Error)
	err := db.Create(&compatItem{ID: "2", Name: "dup"}).Error
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
}

// TestCompatPrimaryKeyViolationTranslated 验证 PK 冲突也翻译为 ErrDuplicatedKey。
func TestCompatPrimaryKeyViolationTranslated(t *testing.T) {
	db := newCompatDB(t)
	require.NoError(t, db.Create(&compatItem{ID: "pk", Name: "first"}).Error)
	err := db.Create(&compatItem{ID: "pk", Name: "second"}).Error
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
}

// TestCompatRecordNotFound 验证查询不到行返回 gorm.ErrRecordNotFound。
func TestCompatRecordNotFound(t *testing.T) {
	db := newCompatDB(t)
	var got compatItem
	err := db.First(&got, "id = ?", "missing").Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// TestCompatSavepoint 验证 SAVEPOINT / ROLLBACK TO SAVEPOINT。
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

// TestCompatForeignKeyViolationTranslated 验证 FK 冲突翻译为 ErrForeignKeyViolated。
func TestCompatForeignKeyViolationTranslated(t *testing.T) {
	type parent struct {
		ID string `gorm:"primaryKey"`
	}
	type child struct {
		ID       string `gorm:"primaryKey"`
		ParentID string `gorm:"index"`
	}
	db, err := gorm.Open(sqlitedialect.Open("file:"+t.TempDir()+"/fk.db?cache=shared&_pragma=foreign_keys(1)"),
		&gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 手动建表以声明 FK（GORM tag 方式声明 FK 较繁琐，这里用 Exec）
	require.NoError(t, db.Exec(`CREATE TABLE parents (id TEXT PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE children (
		id TEXT PRIMARY KEY,
		parent_id TEXT NOT NULL,
		FOREIGN KEY (parent_id) REFERENCES parents(id))`).Error)

	// 插入指向不存在 parent 的 child → FK 冲突
	err = db.Exec(`INSERT INTO children (id, parent_id) VALUES ('c1', 'nonexistent')`).Error
	// raw Exec 不走 TranslateError（TranslateError 只在 GORM CRUD 路径）；
	// 用 GORM Create 走翻译路径：
	type childRow struct {
		ID       string `gorm:"table:children;primaryKey"`
		ParentID string `gorm:"column:parent_id"`
	}
	err = db.Table("children").Create(&childRow{ID: "c2", ParentID: "ghost"}).Error
	require.ErrorIs(t, err, gorm.ErrForeignKeyViolated)
}

// TestCompatUpdateAndDelete 验证更新与删除路径。
func TestCompatUpdateAndDelete(t *testing.T) {
	db := newCompatDB(t)
	require.NoError(t, db.Create(&compatItem{ID: "u", Name: "old", Ts: time.Now()}).Error)
	require.NoError(t, db.Model(&compatItem{}).Where("id = ?", "u").Update("name", "new").Error)
	var got compatItem
	require.NoError(t, db.First(&got, "id = ?", "u").Error)
	require.Equal(t, "new", got.Name)

	require.NoError(t, db.Where("id = ?", "u").Delete(&compatItem{}).Error)
	err := db.First(&got, "id = ?", "u").Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

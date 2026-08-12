package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// TestSQLiteVecRegistered 验证 modernc.org/sqlite/vec 在纯 Go（CGO_ENABLED=0）下
// 通过空导入自动注册到每个新连接，且 vec_distance_cosine 可用。
// spec §3 要求实现前先验证 vec 函数，不能只以编译通过作为证据。
func TestSQLiteVecRegistered(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存 SQLite 失败: %v", err)
	}
	defer db.Close()

	// 正交单位向量余弦距离应为 1.0
	var dist float64
	err = db.QueryRow(`SELECT vec_distance_cosine(vec_f32('[1,0,0]'), vec_f32('[0,1,0]'))`).Scan(&dist)
	if err != nil {
		t.Fatalf("vec_distance_cosine 不可用（vec 扩展未注册）: %v", err)
	}
	if dist < 0.99 || dist > 1.01 {
		t.Fatalf("正交向量 cosine distance = %v, want ~1.0", dist)
	}

	// 相同向量余弦距离应为 0.0
	var zero float64
	err = db.QueryRow(`SELECT vec_distance_cosine(vec_f32('[1,2,3]'), vec_f32('[1,2,3]'))`).Scan(&zero)
	if err != nil {
		t.Fatalf("vec_distance_cosine 失败: %v", err)
	}
	if zero > 1e-6 {
		t.Fatalf("相同向量 cosine distance = %v, want ~0.0", zero)
	}
}

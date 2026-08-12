//go:build integration

// Package db_test 的外部测试包用于 SQLite retrieval 方言分发集成测试。
//
// 放在 db_test（而非 db）是因为测试需要注入真实的 gse 分词器实现 db.SearchTokenizer，
// 而 gse adapter 自身 import db：内部测试包 db → gse → db 会形成 import cycle；
// 外部测试包 db_test → {db, gse}、gse → db 不构成环（spec §8.2）。
package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/adapters/tokenizer/gse"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	indexport "github.com/dajee/langhuan/internal/ports/index"
	"github.com/dajee/langhuan/internal/testsupport"
	"gorm.io/gorm"
)

// sqliteDim 选用最小支持维度，保持 BLOB 体积小、测试快。
const sqliteDim = 798

// openSQLiteRetrievalDB 打开临时 SQLite 库并迁移到最新 schema，关闭外键以便直接
// 插入 retrieval_entries 夹具（其上游 lineage FK 链较重，与投影/检索无关）。
// 单连接池（MaxOpenConns=1）保证 PRAGMA 在同一连接持续生效。
func openSQLiteRetrievalDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testsupport.SQLiteTestDSN(t)
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: dsn}))
	database, dialect, err := db.Open(config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
	require.NoError(t, err)
	require.Equal(t, db.DialectSQLite, dialect)
	require.NoError(t, database.Exec("PRAGMA foreign_keys=OFF").Error)
	return database
}

// newSQLiteTokenizer 加载 gse 内嵌词典，整个测试进程共享一个分词器实例。
func newSQLiteTokenizer(t *testing.T) db.SearchTokenizer {
	t.Helper()
	segmenter, err := gse.New()
	require.NoError(t, err)
	return segmenter
}

// sqliteStageEntry 构造一个合法的 staging RetrievalEntry（所有 lineage UUID 非空、
// State=staging、SearchContent/Content 非空、SourceAnchor 通过校验）。
func sqliteStageEntry(t *testing.T, workspaceID, kbID, generationID uuid.UUID, searchContent string) *model.RetrievalEntry {
	t.Helper()
	return &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		IndexGenerationID:  generationID,
		DocumentID:         uuid.New(),
		DocumentRevisionID: uuid.New(),
		ChunkSetID:         uuid.New(),
		ChunkID:            uuid.New(),
		ChunkRevisionID:    uuid.New(),
		State:              value.RetrievalEntryStaging,
		SearchContent:      searchContent,
		Content:            searchContent,
		SourceAnchor:       value.SourceAnchor{SourceType: "test"},
		Metadata:           map[string]any{},
		CreatedAt:          time.Now().UTC(),
	}
}

// sqlitePublishEntries 把指定 entry 置为 published，使检索 WHERE state='published' 命中。
func sqlitePublishEntries(t *testing.T, database *gorm.DB, workspaceID uuid.UUID, entryIDs []uuid.UUID) {
	t.Helper()
	require.NotEmpty(t, entryIDs)
	require.NoError(t, database.Exec(
		"UPDATE retrieval_entries SET state = ? WHERE workspace_id = ? AND id IN ?",
		string(value.RetrievalEntryPublished), workspaceID, entryIDs,
	).Error)
}

// sqliteVector 在 axis 分量处置 1，其余为 0；构造正交单位向量用于余弦距离断言。
func sqliteVector(axis int) []float32 {
	v := make([]float32, sqliteDim)
	v[axis] = 1
	return v
}

// sqliteRunSearch 在一个 Workspace 事务内运行 reader 并收集候选。
func sqliteRunSearch(
	t *testing.T,
	ctx context.Context,
	repo *db.RetrievalRepository,
	workspaceID uuid.UUID,
	search func(indexport.SearchReader) ([]indexport.SearchCandidate, error),
) []indexport.SearchCandidate {
	t.Helper()
	var candidates []indexport.SearchCandidate
	require.NoError(t, repo.WithinWorkspace(ctx, workspaceID, func(ctx context.Context, reader indexport.SearchReader) error {
		got, err := search(reader)
		candidates = got
		return err
	}))
	return candidates
}

// TestSQLiteStageBatchWritesProjections 验证 StageBatch 在 SQLite 下写入三张表：
// retrieval_entries（不含 embedding/fts_document）、retrieval_embeddings（BLOB）、
// retrieval_fts（gse 分词后的 tokenized 文本）。
func TestSQLiteStageBatchWritesProjections(t *testing.T) {
	database := openSQLiteRetrievalDB(t)
	tokenizer := newSQLiteTokenizer(t)
	ctx := context.Background()
	wsID, kbID, genID := uuid.New(), uuid.New(), uuid.New()
	repo := db.NewRetrievalRepository(database, tokenizer)

	entry := sqliteStageEntry(t, wsID, kbID, genID, "退款流程说明")
	require.NoError(t, repo.StageBatch(ctx, wsID, "simple", sqliteDim, []indexport.StageEntry{
		{Entry: entry, Embedding: sqliteVector(0)},
	}))

	// retrieval_entries：行存在，dimension 已回填，且无 embedding/fts_document 列污染。
	var row db.RetrievalEntryRow
	require.NoError(t, database.First(&row, "workspace_id = ? AND id = ?", wsID, entry.ID).Error)
	require.Equal(t, string(value.RetrievalEntryStaging), row.State)
	require.NotNil(t, row.Dimension)
	require.Equal(t, sqliteDim, *row.Dimension)

	// retrieval_embeddings：维度与 BLOB 体积正确。
	var emb struct {
		Dimension int
		Embedding []byte `gorm:"column:embedding"`
	}
	require.NoError(t, database.Table("retrieval_embeddings").
		Select("dimension, embedding").Where("entry_id = ?", entry.ID).Scan(&emb).Error)
	require.Equal(t, sqliteDim, emb.Dimension)
	require.Len(t, emb.Embedding, 4*sqliteDim)

	// retrieval_fts：写入 tokenized 文本（gse 切分为多个 token，以空格连接）。
	var fts struct {
		Tokenized string `gorm:"column:search_content_tokenized"`
	}
	require.NoError(t, database.Table("retrieval_fts").
		Select("search_content_tokenized").Where("entry_id = ?", entry.ID).Scan(&fts).Error)
	require.NotEmpty(t, fts.Tokenized)
	require.Contains(t, fts.Tokenized, "退款")
}

// TestSQLiteVectorCandidatesRanksByCosine 验证 SQLite 向量检索按余弦距离升序返回，
// 且严格隔离 workspace：相邻向量在前、正交向量在后，跨租户条目不出现。
func TestSQLiteVectorCandidatesRanksByCosine(t *testing.T) {
	database := openSQLiteRetrievalDB(t)
	tokenizer := newSQLiteTokenizer(t)
	ctx := context.Background()
	wsA, kbA, genA := uuid.New(), uuid.New(), uuid.New()
	wsB, kbB, genB := uuid.New(), uuid.New(), uuid.New()
	repo := db.NewRetrievalRepository(database, tokenizer)

	// workspace A：near 与查询同向（distance 0），far 正交（distance 1）。
	near := sqliteStageEntry(t, wsA, kbA, genA, "近邻正文")
	far := sqliteStageEntry(t, wsA, kbA, genA, "远端正文")
	require.NoError(t, repo.StageBatch(ctx, wsA, "simple", sqliteDim, []indexport.StageEntry{
		{Entry: near, Embedding: sqliteVector(0)},
		{Entry: far, Embedding: sqliteVector(1)},
	}))
	// workspace B：与查询同向，用于验证不串租户。
	cross := sqliteStageEntry(t, wsB, kbB, genB, "跨租户正文")
	require.NoError(t, repo.StageBatch(ctx, wsB, "simple", sqliteDim, []indexport.StageEntry{
		{Entry: cross, Embedding: sqliteVector(0)},
	}))
	sqlitePublishEntries(t, database, wsA, []uuid.UUID{near.ID, far.ID})
	sqlitePublishEntries(t, database, wsB, []uuid.UUID{cross.ID})

	query := sqliteVector(0)
	candidates := sqliteRunSearch(t, ctx, repo, wsA, func(reader indexport.SearchReader) ([]indexport.SearchCandidate, error) {
		return reader.VectorCandidates(ctx, indexport.SearchRequest{
			KnowledgeBaseID: kbA, GenerationID: genA,
			Query: "向量检索", QueryEmbedding: query,
			FTSConfig: "simple", Dimension: sqliteDim,
			VectorTopK: 10, KeywordTopK: 10,
		})
	})

	require.Len(t, candidates, 2)
	require.Equal(t, near.ID, candidates[0].EntryID)
	require.Equal(t, far.ID, candidates[1].EntryID)
	// near 与查询同向 → score≈1；far 正交 → score≈0。
	require.InDelta(t, 1.0, candidates[0].Score, 1e-6)
	require.InDelta(t, 0.0, candidates[1].Score, 1e-6)
	for _, candidate := range candidates {
		require.NotEqual(t, cross.ID, candidate.EntryID, "跨 workspace 条目泄漏")
	}
}

// TestSQLiteKeywordCandidatesMatchesChineseFTS 验证 SQLite FTS5 中文检索：
// gse 分词写入 tokenized 文本，查询同样分词后构造 AND MATCH 命中，并按 bm25 排序，
// 干扰条目不命中。
func TestSQLiteKeywordCandidatesMatchesChineseFTS(t *testing.T) {
	database := openSQLiteRetrievalDB(t)
	tokenizer := newSQLiteTokenizer(t)
	ctx := context.Background()
	wsID, kbID, genID := uuid.New(), uuid.New(), uuid.New()
	repo := db.NewRetrievalRepository(database, tokenizer)

	match := sqliteStageEntry(t, wsID, kbID, genID, "用户退款流程说明文档")
	distractor := sqliteStageEntry(t, wsID, kbID, genID, "天气预报与空气质量指数")
	require.NoError(t, repo.StageBatch(ctx, wsID, "simple", sqliteDim, []indexport.StageEntry{
		{Entry: match, Embedding: sqliteVector(0)},
		{Entry: distractor, Embedding: sqliteVector(0)},
	}))
	sqlitePublishEntries(t, database, wsID, []uuid.UUID{match.ID, distractor.ID})

	candidates := sqliteRunSearch(t, ctx, repo, wsID, func(reader indexport.SearchReader) ([]indexport.SearchCandidate, error) {
		return reader.KeywordCandidates(ctx, indexport.SearchRequest{
			KnowledgeBaseID: kbID, GenerationID: genID,
			Query: "退款", QueryEmbedding: sqliteVector(0),
			FTSConfig: "simple", Dimension: sqliteDim,
			VectorTopK: 10, KeywordTopK: 10,
		})
	})

	require.Len(t, candidates, 1)
	require.Equal(t, match.ID, candidates[0].EntryID)
}

// TestSQLiteKeywordCandidatesEmptyQueryReturnsNone 验证分词后无 token 时短路返回空候选，
// 不构造空 MATCH 表达式（spec §8.2：无 token 返回空）。
func TestSQLiteKeywordCandidatesEmptyQueryReturnsNone(t *testing.T) {
	database := openSQLiteRetrievalDB(t)
	tokenizer := newSQLiteTokenizer(t)
	ctx := context.Background()
	wsID, kbID, genID := uuid.New(), uuid.New(), uuid.New()
	repo := db.NewRetrievalRepository(database, tokenizer)

	entry := sqliteStageEntry(t, wsID, kbID, genID, "退款流程")
	require.NoError(t, repo.StageBatch(ctx, wsID, "simple", sqliteDim, []indexport.StageEntry{
		{Entry: entry, Embedding: sqliteVector(0)},
	}))
	sqlitePublishEntries(t, database, wsID, []uuid.UUID{entry.ID})

	// 仅标点/空白：gse 切不出 token，应短路返回空候选而非执行 MATCH。
	candidates := sqliteRunSearch(t, ctx, repo, wsID, func(reader indexport.SearchReader) ([]indexport.SearchCandidate, error) {
		return reader.KeywordCandidates(ctx, indexport.SearchRequest{
			KnowledgeBaseID: kbID, GenerationID: genID,
			Query: "  ， 。 ", QueryEmbedding: sqliteVector(0),
			FTSConfig: "simple", Dimension: sqliteDim,
			VectorTopK: 10, KeywordTopK: 10,
		})
	})
	require.Empty(t, candidates)
}

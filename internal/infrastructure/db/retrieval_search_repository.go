package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// WithinWorkspace binds all candidate and evidence reads to one tenant-local transaction.
func (r *RetrievalRepository) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, indexport.SearchReader) error,
) error {
	if fn == nil {
		return fmt.Errorf("%w: Retrieval search callback 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &retrievalSearchReader{db: tx, workspaceID: workspaceID})
	})
}

type retrievalSearchReader struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

// GetActiveGeneration resolves the only searchable Generation through the KB pointer.
func (r *retrievalSearchReader) GetActiveGeneration(
	ctx context.Context,
	knowledgeBaseID uuid.UUID,
) (*model.IndexGeneration, error) {
	var row IndexGenerationRow
	if err := r.db.WithContext(ctx).Table("knowledge_base_index_generations AS g").Select("g.*").Joins(
		"JOIN knowledge_bases AS kb ON kb.workspace_id = g.workspace_id "+
			"AND kb.id = g.knowledge_base_id AND kb.active_index_generation_id = g.id",
	).Where(
		"g.workspace_id = ? AND g.knowledge_base_id = ? AND kb.deleted_at IS NULL",
		r.workspaceID, knowledgeBaseID,
	).First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取 active IndexGeneration 失败")
	}
	return indexGenerationFromRow(&row), nil
}

// VectorCandidates executes one of four fixed HNSW-compatible expressions.
func (r *retrievalSearchReader) VectorCandidates(
	ctx context.Context,
	request indexport.SearchRequest,
) ([]indexport.SearchCandidate, error) {
	if err := validateSearchRequest(request, true); err != nil {
		return nil, err
	}
	query, ok := vectorSearchSQL[request.Dimension]
	if !ok {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
	}
	vector := halfVectorLiteral(request.QueryEmbedding)
	var rows []searchCandidateRow
	if err := r.db.WithContext(ctx).Raw(
		query,
		vector, r.workspaceID, request.KnowledgeBaseID, request.GenerationID,
		request.Dimension, vector, request.VectorTopK,
	).Scan(&rows).Error; err != nil {
		return nil, translateDBError(err, "执行 Retrieval vector search 失败")
	}
	return searchCandidatesFromRows(rows), nil
}

// KeywordCandidates searches the stored search_content projection through its tsvector.
func (r *retrievalSearchReader) KeywordCandidates(
	ctx context.Context,
	request indexport.SearchRequest,
) ([]indexport.SearchCandidate, error) {
	if err := validateSearchRequest(request, false); err != nil {
		return nil, err
	}
	var rows []searchCandidateRow
	if err := r.db.WithContext(ctx).Raw(
		keywordSearchSQL,
		request.FTSConfig, request.Query,
		r.workspaceID, request.KnowledgeBaseID, request.GenerationID, request.KeywordTopK,
	).Scan(&rows).Error; err != nil {
		return nil, translateFTSConfigError(err, request.FTSConfig, "执行 Retrieval FTS search 失败")
	}
	return searchCandidatesFromRows(rows), nil
}

// LoadEvidence resolves current Document/file-node names for selected published entries.
func (r *retrievalSearchReader) LoadEvidence(
	ctx context.Context,
	knowledgeBaseID, generationID uuid.UUID,
	entryIDs []uuid.UUID,
) ([]indexport.SearchEvidence, error) {
	if knowledgeBaseID == uuid.Nil || generationID == uuid.Nil {
		return nil, fmt.Errorf("%w: Retrieval evidence lineage 无效", domainerrors.ErrValidation)
	}
	if len(entryIDs) == 0 {
		return []indexport.SearchEvidence{}, nil
	}
	var rows []searchEvidenceRow
	if err := r.db.WithContext(ctx).Table("retrieval_entries AS re").Select(
		"re.id AS entry_id, COALESCE(parent.id, child.id) AS chunk_id, COALESCE(parent_revision.id, re.chunk_revision_id) AS chunk_revision_id, re.document_id, "+
			"d.kind AS document_kind, COALESCE(parent_revision.content, re.content) AS content, d.title AS document_title, "+
			"ftn.name AS file_node_name, COALESCE(parent.source_anchor, re.source_anchor) AS source_anchor, COALESCE(parent.metadata, re.metadata) AS metadata, "+
			"re.chunk_id AS matched_chunk_id, re.chunk_revision_id AS matched_chunk_revision_id, re.content AS matched_content, re.source_anchor AS matched_source_anchor, child.role AS matched_role",
	).Joins(
		"JOIN chunks AS child ON child.workspace_id = re.workspace_id AND child.id = re.chunk_id",
	).Joins(
		"LEFT JOIN chunks AS parent ON parent.workspace_id = child.workspace_id AND parent.id = child.parent_chunk_id",
	).Joins(
		"LEFT JOIN chunk_revisions AS parent_revision ON parent_revision.workspace_id = parent.workspace_id AND parent_revision.id = parent.active_revision_id",
	).Joins(
		"JOIN documents AS d ON d.workspace_id = re.workspace_id "+
			"AND d.knowledge_base_id = re.knowledge_base_id AND d.id = re.document_id",
	).Joins(
		"LEFT JOIN file_tree_nodes AS ftn ON ftn.workspace_id = d.workspace_id "+
			"AND ftn.knowledge_base_id = d.knowledge_base_id AND ftn.document_id = d.id "+
			"AND ftn.node_type = 'file'",
	).Where(
		"re.workspace_id = ? AND re.knowledge_base_id = ? AND re.index_generation_id = ? "+
			"AND re.state = ? AND re.id IN ? AND d.deleted_at IS NULL",
		r.workspaceID, knowledgeBaseID, generationID, value.RetrievalEntryPublished, entryIDs,
	).Scan(&rows).Error; err != nil {
		return nil, translateDBError(err, "加载 Retrieval evidence 失败")
	}
	result := make([]indexport.SearchEvidence, len(rows))
	for index, row := range rows {
		kind := value.DocumentKind(row.DocumentKind)
		if err := kind.Validate(); err != nil {
			return nil, fmt.Errorf("Retrieval evidence DocumentKind 无效: %w", err)
		}
		documentName := row.DocumentTitle
		if kind == value.DocumentKindFile {
			if row.FileNodeName == nil || *row.FileNodeName != row.DocumentTitle {
				return nil, fmt.Errorf("%w: File Document 与文件节点名称不一致", domainerrors.ErrConflict)
			}
			documentName = *row.FileNodeName
		}
		anchor, err := sourceAnchorFromJSONMap(row.SourceAnchor)
		if err != nil {
			return nil, err
		}
		matchedAnchor, err := sourceAnchorFromJSONMap(row.MatchedSourceAnchor)
		if err != nil {
			return nil, err
		}
		matchedRole := value.ChunkRole(row.MatchedRole)
		if err := matchedRole.Validate(); err != nil {
			return nil, fmt.Errorf("Retrieval evidence matched role 无效: %w", err)
		}
		result[index] = indexport.SearchEvidence{
			EntryID: row.EntryID, ChunkID: row.ChunkID, ChunkRevisionID: row.ChunkRevisionID,
			DocumentID: row.DocumentID, DocumentKind: kind, Content: row.Content,
			DocumentName: documentName, SourceAnchor: anchor,
			Metadata:       normalizedDomainMap(row.Metadata),
			MatchedChunkID: row.MatchedChunkID, MatchedChunkRevisionID: row.MatchedChunkRevisionID,
			MatchedContent: row.MatchedContent, MatchedSourceAnchor: matchedAnchor, MatchedRole: matchedRole,
		}
	}
	return result, nil
}

type searchCandidateRow struct {
	EntryID uuid.UUID
	Score   float64
}

type searchEvidenceRow struct {
	EntryID, ChunkID, ChunkRevisionID, DocumentID uuid.UUID
	MatchedChunkID, MatchedChunkRevisionID        uuid.UUID
	DocumentKind, Content, DocumentTitle          string
	MatchedContent, MatchedRole                   string
	FileNodeName                                  *string
	SourceAnchor, Metadata, MatchedSourceAnchor   JSONMap
}

func searchCandidatesFromRows(rows []searchCandidateRow) []indexport.SearchCandidate {
	result := make([]indexport.SearchCandidate, len(rows))
	for index, row := range rows {
		result[index] = indexport.SearchCandidate{EntryID: row.EntryID, Score: row.Score}
	}
	return result
}

func validateSearchRequest(request indexport.SearchRequest, vector bool) error {
	if request.KnowledgeBaseID == uuid.Nil || request.GenerationID == uuid.Nil ||
		strings.TrimSpace(request.Query) == "" || strings.TrimSpace(request.FTSConfig) == "" ||
		!value.IsSupportedEmbeddingDimension(request.Dimension) ||
		request.VectorTopK < 1 || request.KeywordTopK < 1 {
		return fmt.Errorf("%w: Retrieval search request 无效", domainerrors.ErrValidation)
	}
	if vector && len(request.QueryEmbedding) != request.Dimension {
		return domainerrors.ErrDimensionMismatch
	}
	return nil
}

var vectorSearchSQL = map[int]string{
	798:  vectorSearch798SQL,
	1024: vectorSearch1024SQL,
	2048: vectorSearch2048SQL,
	3584: vectorSearch3584SQL,
}

const vectorSearch798SQL = "SELECT id AS entry_id, 1 - distance AS score FROM (" +
	"SELECT id, ((embedding::halfvec(798)) <=> (?::halfvec(798))) AS distance FROM retrieval_entries " +
	"WHERE workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ? " +
	"AND state = 'published' AND dimension = ? " +
	"ORDER BY (embedding::halfvec(798)) <=> (?::halfvec(798)) LIMIT ?" +
	") AS candidates ORDER BY distance ASC, id ASC"

const vectorSearch1024SQL = "SELECT id AS entry_id, 1 - distance AS score FROM (" +
	"SELECT id, ((embedding::halfvec(1024)) <=> (?::halfvec(1024))) AS distance FROM retrieval_entries " +
	"WHERE workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ? " +
	"AND state = 'published' AND dimension = ? " +
	"ORDER BY (embedding::halfvec(1024)) <=> (?::halfvec(1024)) LIMIT ?" +
	") AS candidates ORDER BY distance ASC, id ASC"

const vectorSearch2048SQL = "SELECT id AS entry_id, 1 - distance AS score FROM (" +
	"SELECT id, ((embedding::halfvec(2048)) <=> (?::halfvec(2048))) AS distance FROM retrieval_entries " +
	"WHERE workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ? " +
	"AND state = 'published' AND dimension = ? " +
	"ORDER BY (embedding::halfvec(2048)) <=> (?::halfvec(2048)) LIMIT ?" +
	") AS candidates ORDER BY distance ASC, id ASC"

const vectorSearch3584SQL = "SELECT id AS entry_id, 1 - distance AS score FROM (" +
	"SELECT id, ((embedding::halfvec(3584)) <=> (?::halfvec(3584))) AS distance FROM retrieval_entries " +
	"WHERE workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ? " +
	"AND state = 'published' AND dimension = ? " +
	"ORDER BY (embedding::halfvec(3584)) <=> (?::halfvec(3584)) LIMIT ?" +
	") AS candidates ORDER BY distance ASC, id ASC"

const keywordSearchSQL = "WITH search_query AS (SELECT plainto_tsquery(?::regconfig, ?) AS value) " +
	"SELECT re.id AS entry_id, ts_rank_cd(re.fts_document, search_query.value) AS score " +
	"FROM retrieval_entries AS re CROSS JOIN search_query " +
	"WHERE re.workspace_id = ? AND re.knowledge_base_id = ? AND re.index_generation_id = ? " +
	"AND re.state = 'published' AND re.fts_document @@ search_query.value " +
	"ORDER BY score DESC, re.id ASC LIMIT ?"

var _ indexport.SearchRepository = (*RetrievalRepository)(nil)

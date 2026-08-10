# 检索证据血缘与可回放检索 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** 为每次检索建立稳定 SearchRun 身份、完整 Evidence lineage、可验证 CitationRef，并提供不向公开 API 暴露 Generation ID 的 Workspace 管理员回放入口。

**Architecture:** 应用层统一返回 dto.SearchResponse；SearchRun Store 持久化运行元数据和 Generation snapshot；单库 REST 继续返回数组，多库 REST/MCP 增量扩展 wrapper。普通搜索只使用 active Generation，只有内部 ReplayService 可以传入固定快照，每次回放创建新的 SearchRun。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL 17 + pgvector/zhparser、MCP over HTTP、OpenTelemetry、golang-migrate、testcontainers。

## Global Constraints

- Domain 不依赖 HTTP、GORM、MCP 或第三方 SDK；Repository 接口定义在 application 使用方。
- 数据库访问必须进入 Workspace transaction 并显式带 workspace_id。
- SearchRun 不保存原始 query、正文、向量、API Key secret 或完整第三方响应。
- score 保持 RRF 语义；rerank_score 和 ranking_stage 不变；不增加 final_score。
- 单库 REST body 保持 []SearchResult；公开 API 不接受 generation_id。
- 数据库测试只使用临时 langhuan-test-postgres:pg17 容器。
- SearchRun 默认保留 168 小时，且不得长于 retired_generation_retention；配置键为 retrieval.search_run_retention。
- 每个任务均采用 TDD，并以中文 Conventional Commit 独立提交。

---

### Task 1: 建立 SearchRun、CitationRef 与 Evidence lineage 类型合同

**Files:**
- Create: internal/domain/value/search_run.go
- Create: internal/domain/value/search_run_test.go
- Create: internal/application/dto/search_run.go
- Modify: internal/application/dto/search.go
- Modify: internal/ports/index/index.go
- Modify: internal/infrastructure/db/retrieval_search_repository.go
- Test: internal/application/dto/search_test.go
- Test: internal/infrastructure/db/retrieval_search_integration_test.go

**Interfaces:**
- Produces: value.RetrievalStatus、value.CitationStatus、value.SearchScope。
- Produces: dto.SearchResponse、dto.SearchRunSummary、dto.GenerationSnapshot、dto.CitationRef。
- Produces: index.SearchEvidence.DocumentRevisionID。

- [x] Step 1: 写状态值失败测试

    func TestRetrievalStatusValidate(t *testing.T) {
        valid := []RetrievalStatus{
            RetrievalStatusRunning,
            RetrievalStatusAvailable,
            RetrievalStatusEmpty,
            RetrievalStatusDegraded,
            RetrievalStatusFailed,
        }
        for _, status := range valid {
            require.NoError(t, status.Validate())
        }
        require.ErrorIs(t, RetrievalStatus("bad").Validate(), domainerrors.ErrValidation)
    }

Run: go test ./internal/domain/value -run TestRetrievalStatusValidate -count=1  
Expected: FAIL，类型尚未定义。

- [x] Step 2: 实现领域值

    type RetrievalStatus string
    const (
        RetrievalStatusRunning RetrievalStatus = "running"
        RetrievalStatusAvailable RetrievalStatus = "available"
        RetrievalStatusEmpty RetrievalStatus = "empty"
        RetrievalStatusDegraded RetrievalStatus = "degraded"
        RetrievalStatusFailed RetrievalStatus = "failed"
    )

同时实现 CitationStatusValid、CitationStatusUnavailable、SearchScopeSelected、SearchScopeAPIKeyBoundAll 及 Validate。

- [x] Step 3: 写 DTO lineage 失败测试

    func TestSearchResultCarriesProjectionLineage(t *testing.T) {
        docRev := uuid.New()
        generation := uuid.New()
        result := SearchResultFromEvidence(indexport.SearchEvidence{
            DocumentRevisionID: docRev,
            ChunkRevisionID: uuid.New(),
            Content: "退款将在三个工作日内到账",
        }, generation, 0.031, nil, nil)
        require.Equal(t, docRev, result.DocumentRevisionID)
        require.Equal(t, generation, result.IndexGenerationID)
    }

Run: go test ./internal/application/dto -run TestSearchResultCarriesProjectionLineage -count=1  
Expected: FAIL，DTO 和构造函数尚未扩展。

- [x] Step 4: 实现 DTO 和 Repository 投影

SearchResult 增加 DocumentRevisionID、IndexGenerationID、Citation；CitationRef 包含 DocumentRevisionID、ChunkRevisionID、SourceAnchor、ContentSHA256、Status。SearchEvidence 增加 DocumentRevisionID，SQL 必须选择 retrieval_entries.document_revision_id，不从 Document active pointer 推导。

SearchResultFromEvidence 签名固定为：

    func SearchResultFromEvidence(
        evidence indexport.SearchEvidence,
        generationID uuid.UUID,
        score float64,
        vectorScore, keywordScore *float64,
    ) *SearchResult

- [x] Step 5: 运行单元与集成测试

Run: go test ./internal/domain/value ./internal/application/dto ./internal/ports/index -count=1  
Expected: PASS。

Run: make test-image && go test -tags=integration ./internal/infrastructure/db -run TestRetrievalSearch -count=1  
Expected: PASS，Document Revision 与 seed retrieval entry 一致。

- [x] Step 6: 提交

    git add internal/domain/value/search_run.go internal/domain/value/search_run_test.go internal/application/dto/search_run.go internal/application/dto/search.go internal/application/dto/search_test.go internal/ports/index/index.go internal/infrastructure/db/retrieval_search_repository.go internal/infrastructure/db/retrieval_search_integration_test.go
    git commit -m "feat(search): 增加检索证据血缘合同"

### Task 2: 固化 query/content fingerprint 与失败分类

**Files:**
- Create: internal/application/service/search_fingerprint.go
- Create: internal/application/service/search_fingerprint_test.go
- Create: internal/application/service/search_status.go
- Create: internal/application/service/search_status_test.go
- Modify: internal/application/service/search_observability.go
- Modify: internal/application/dto/search.go

**Interfaces:**
- Produces: canonicalSearchQuery、searchQueryHash、evidenceContentSHA256。
- Produces: classifySearchFailure(error, searchFailurePhase) string。

- [x] Step 1: 写 fingerprint 失败测试

    func TestSearchQueryHashUsesTrimmedUTF8Query(t *testing.T) {
        got := searchQueryHash(" 退款政策 ")
        sum := sha256.Sum256([]byte("退款政策"))
        require.Equal(t, "sha256:v1:"+hex.EncodeToString(sum[:]), got)
    }

    func TestEvidenceContentSHA256HashesExactBytes(t *testing.T) {
        content := "第一行\n第二行"
        sum := sha256.Sum256([]byte(content))
        require.Equal(t, hex.EncodeToString(sum[:]), evidenceContentSHA256(content))
    }

Run: go test ./internal/application/service -run 'Test(SearchQueryHash|EvidenceContentSHA256)' -count=1  
Expected: FAIL。

- [x] Step 2: 实现 fingerprint

    func canonicalSearchQuery(raw string) string {
        return strings.TrimSpace(raw)
    }

    func searchQueryHash(raw string) string {
        sum := sha256.Sum256([]byte(canonicalSearchQuery(raw)))
        return "sha256:v1:" + hex.EncodeToString(sum[:])
    }

    func evidenceContentSHA256(content string) string {
        sum := sha256.Sum256([]byte(content))
        return hex.EncodeToString(sum[:])
    }

SearchResultFromEvidence 对实际返回 Content 计算 hash，并设置 CitationStatusValid。

- [x] Step 3: 写 phase-aware 分类测试

    func TestClassifySearchFailureByPhase(t *testing.T) {
        require.Equal(t, "embedding_timeout",
            classifySearchFailure(domainerrors.ErrRequestTimeout, searchFailurePhaseEmbedding))
        require.Equal(t, "rerank_timeout",
            classifySearchFailure(domainerrors.ErrRequestTimeout, searchFailurePhaseRerank))
        require.Equal(t, "generation_not_ready",
            classifySearchFailure(domainerrors.ErrGenerationNotReady, searchFailurePhaseRetrieval))
    }

- [x] Step 4: 实现分类并接入日志

定义 validation、embedding、retrieval、rerank 四个 phase。映射 EndpointUnreachable、RequestTimeout、RateLimited、无效 embedding/rerank response、Generation 错误和 snapshot mismatch。search_observability.go 与 SearchRun 必须复用同一 classifier。

Run: go test ./internal/application/dto ./internal/application/service -run 'Test(SearchQueryHash|EvidenceContentSHA256|ClassifySearchFailure|SearchResult)' -count=1  
Expected: PASS。

- [x] Step 5: 提交

    git add internal/application/service/search_fingerprint.go internal/application/service/search_fingerprint_test.go internal/application/service/search_status.go internal/application/service/search_status_test.go internal/application/service/search_observability.go internal/application/dto/search.go
    git commit -m "feat(search): 固化检索指纹和失败分类"

### Task 3: 新增 SearchRun migration、模型与 Repository

**Files:**
- Create: internal/infrastructure/migrate/migrations/000023_search_runs.up.sql
- Create: internal/infrastructure/migrate/migrations/000023_search_runs.down.sql
- Create: internal/domain/model/search_run.go
- Create: internal/domain/model/search_run_test.go
- Create: internal/application/service/search_run_store.go
- Create: internal/infrastructure/db/search_run_rows.go
- Create: internal/infrastructure/db/search_run_repository.go
- Create: internal/infrastructure/db/search_run_repository_integration_test.go
- Create: internal/infrastructure/migrate/migrate_v023_search_runs_integration_test.go

**Interfaces:**
- Produces: model.SearchRun、model.SearchRunGeneration、model.SearchRunCompletion。
- Produces: SearchRunStore.Create、Complete、Get、DeleteExpired。

- [x] Step 1: 写 migration 失败测试

测试从空库迁移后 search_runs 和 search_run_generations 两张表存在；跨 Workspace generation snapshot 外键被拒绝；replay_of_id 只能指向同 Workspace SearchRun。

Run: make test-image && go test -tags=integration ./internal/infrastructure/migrate -run TestMigrateV023SearchRuns -count=1  
Expected: FAIL，migration 尚不存在。

- [x] Step 2: 实现 up/down SQL

up migration 至少包含以下结构，具体 CHECK 文本必须与领域值对象一致：

    CREATE TABLE search_runs (
        id uuid PRIMARY KEY,
        workspace_id uuid NOT NULL,
        requested_scope text NOT NULL CHECK (requested_scope IN ('selected', 'api_key_bound_all')),
        query_hash text NOT NULL,
        query_chars integer NOT NULL CHECK (query_chars >= 0),
        vector_top_k integer NOT NULL CHECK (vector_top_k > 0),
        keyword_top_k integer NOT NULL CHECK (keyword_top_k > 0),
        final_top_k integer NOT NULL CHECK (final_top_k > 0),
        retrieval_status text NOT NULL CHECK (retrieval_status IN ('running', 'available', 'empty', 'degraded', 'failed')),
        failure_class text NOT NULL DEFAULT '',
        ranking_stage text NOT NULL DEFAULT '',
        result_count integer NOT NULL DEFAULT 0 CHECK (result_count >= 0),
        request_id text NOT NULL DEFAULT '',
        transport text NOT NULL DEFAULT '',
        principal_kind text NOT NULL DEFAULT '',
        created_at timestamptz NOT NULL,
        completed_at timestamptz,
        expires_at timestamptz NOT NULL,
        replay_of_id uuid,
        UNIQUE (workspace_id, id),
        FOREIGN KEY (workspace_id, replay_of_id)
            REFERENCES search_runs (workspace_id, id) ON DELETE SET NULL
    );

    CREATE TABLE search_run_generations (
        id uuid PRIMARY KEY,
        workspace_id uuid NOT NULL,
        search_run_id uuid NOT NULL,
        knowledge_base_id uuid NOT NULL,
        generation_id uuid NOT NULL,
        source_content_version bigint NOT NULL,
        indexed_content_version bigint NOT NULL,
        generation_config_hash text NOT NULL,
        embedding_model_id uuid NOT NULL,
        provider_id uuid NOT NULL,
        model_name text NOT NULL,
        model_config_hash text NOT NULL,
        embedding_dimension integer NOT NULL,
        retrieval_config_hash text NOT NULL,
        rerank_snapshot jsonb,
        FOREIGN KEY (workspace_id, search_run_id)
            REFERENCES search_runs (workspace_id, id) ON DELETE CASCADE,
        FOREIGN KEY (workspace_id, knowledge_base_id, generation_id)
            REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
            ON DELETE RESTRICT,
        UNIQUE (workspace_id, search_run_id, knowledge_base_id)
    );

    CREATE INDEX search_runs_expiry_idx ON search_runs (workspace_id, expires_at);
    CREATE INDEX search_run_generations_lookup_idx ON search_run_generations (workspace_id, search_run_id);

failed status 必须有非空 failure_class，其他终态 failure_class 必须为空；SearchRun retention 不得超过 retired_generation_retention。down 按子表、主表逆序删除。

- [x] Step 3: 定义 Store 接口

    type SearchRunStore interface {
        Create(context.Context, *model.SearchRun) error
        Complete(context.Context, uuid.UUID, uuid.UUID, model.SearchRunCompletion) error
        Get(context.Context, uuid.UUID, uuid.UUID) (*model.SearchRun, error)
        DeleteExpired(context.Context, time.Time, int) (int64, error)
    }

Complete 在 Workspace transaction 中锁行，只允许 running 到终态。Not Found 映射为 domainerrors.ErrNotFound。

- [x] Step 4: 写 Repository 生命周期测试

    func TestSearchRunRepositoryLifecycleAndIsolation(t *testing.T) {
        repo, wsA, wsB, generation := newSearchRunRepositoryHarness(t)
        run := readyRunningSearchRun(wsA, generation)
        require.NoError(t, repo.Create(context.Background(), run))
        require.NoError(t, repo.Complete(context.Background(), wsA, run.ID,
            model.SearchRunCompletion{Status: value.RetrievalStatusAvailable, ResultCount: 2}))
        _, err := repo.Get(context.Background(), wsB, run.ID)
        require.ErrorIs(t, err, domainerrors.ErrNotFound)
    }

另测重复 completion、跨 Workspace snapshot、只删除 expires_at 已过期记录。

Run: make test-image && go test -tags=integration ./internal/infrastructure/migrate ./internal/infrastructure/db -run 'Test(MigrateV023|SearchRunRepository)' -count=1  
Expected: PASS。

- [x] Step 5: 提交

    git add internal/infrastructure/migrate/migrations/000023_search_runs.up.sql internal/infrastructure/migrate/migrations/000023_search_runs.down.sql internal/infrastructure/migrate/migrate_v023_search_runs_integration_test.go internal/domain/model/search_run.go internal/domain/model/search_run_test.go internal/application/service/search_run_store.go internal/infrastructure/db/search_run_rows.go internal/infrastructure/db/search_run_repository.go internal/infrastructure/db/search_run_repository_integration_test.go
    git commit -m "feat(db): 持久化检索运行快照"

### Task 4: 用 recorder 接入单知识库搜索

**Files:**
- Create: internal/application/service/search_run_recorder.go
- Create: internal/application/service/search_run_recorder_test.go
- Modify: internal/application/service/search.go
- Modify: internal/application/service/search_test.go
- Modify: internal/application/service/search_observability.go

**Interfaces:**
- Changes: SearchService.Search 返回 *dto.SearchResponse。
- Produces: newSearchRunRecorder、AddGeneration、Finish。
- Consumes: SearchRunStore 和 Task 2 classifier。

- [x] Step 1: 写 recorder persistence failure 测试

    func TestRecorderPersistenceFailureDoesNotReplaceOutcome(t *testing.T) {
        store := &fakeSearchRunStore{completeErr: errors.New("db unavailable")}
        recorder := newSearchRunRecorder(store, discardLogger(), runningRun())
        recorder.Finish(context.Background(), availableCompletion(1))
        require.Error(t, recorder.PersistenceError())
    }

Run: go test ./internal/application/service -run TestRecorderPersistenceFailure -count=1  
Expected: FAIL。

- [x] Step 2: 实现 recorder

Recorder 创建 running SearchRun、追加 Generation snapshot、完成终态。Create/Complete 失败只记录 search_run_persistence_failed，不覆盖搜索 results/error。

- [x] Step 3: 改造 SearchService

SearchServiceDeps 增加 SearchRuns 和 SearchRunRetention。基础输入校验通过后创建 SearchRun；读取 active Generation 后追加 snapshot；返回 SearchResponse。SearchRunSummary 明确定义 EffectiveScope 和 ReplayOfID。0 结果映射 empty，Rerank fallback 映射 degraded，普通成功映射 available；SearchRun 创建后的错误使用当前 phase classifier 完成 failed，并返回非空 SearchResponse 与原 error。创建前校验错误仍返回 nil response。

- [x] Step 4: 写单库状态矩阵测试

    func TestSearchReturnsRunAndGenerationLineage(t *testing.T) {
        response, err := service.Search(context.Background(), validSearchInput())
        require.NoError(t, err)
        require.NotEqual(t, uuid.Nil, response.Run.SearchID)
        require.Len(t, response.Run.GenerationSnapshots, 1)
        require.Equal(t, response.Run.GenerationSnapshots[0].GenerationID,
            response.Results[0].IndexGenerationID)
    }

增加 empty、fallback、embedding timeout、repository failure、SearchRun persistence failure 场景。

Run: go test ./internal/application/service -run 'Test(Search|Recorder)' -count=1  
Expected: PASS。

- [x] Step 5: 提交

    git add internal/application/service/search_run_recorder.go internal/application/service/search_run_recorder_test.go internal/application/service/search.go internal/application/service/search_test.go internal/application/service/search_observability.go
    git commit -m "feat(search): 记录单库检索运行"

### Task 5: 接入多知识库 SearchRun 与 scope

**Files:**
- Modify: internal/application/service/multi_knowledge_search.go
- Modify: internal/application/service/multi_knowledge_search_test.go
- Modify: internal/application/service/search_rerank.go
- Modify: internal/application/service/multi_knowledge_rerank_test.go

**Interfaces:**
- Changes: MultiKnowledgeSearchService.Search 返回 *dto.SearchResponse。
- Adds: MultiKnowledgeSearchInput.RequestedScope value.SearchScope。
- Consumes: Task 4 recorder。

- [x] Step 1: 写多库 snapshot/scope 测试

    func TestMultiSearchRecordsEffectiveScope(t *testing.T) {
        response, err := service.Search(context.Background(), MultiKnowledgeSearchInput{
            WorkspaceID: workspaceID,
            Access: access,
            KnowledgeBaseIDs: []uuid.UUID{kbA, kbB},
            RequestedScope: value.SearchScopeSelected,
            Query: "退款",
        })
        require.NoError(t, err)
        require.ElementsMatch(t, []uuid.UUID{kbA, kbB},
            response.Run.EffectiveKnowledgeBaseIDs)
        require.Len(t, response.Run.GenerationSnapshots, 2)
    }

Run: go test ./internal/application/service -run TestMultiSearchRecordsEffectiveScope -count=1  
Expected: FAIL。

- [x] Step 2: 接入 recorder

按 KB UUID 稳定排序 Generation snapshots，不依赖 map 遍历。状态规则与单库一致；多库失败保持 all-or-nothing 并完成 failed SearchRun。Application service 不扩展空 IDs。

- [x] Step 3: 增加 requested scope

零值规范化为 selected。MCP 后续在空输入展开 API Key 绑定 IDs，并传 api_key_bound_all；REST 始终传 selected。

Run: go test ./internal/application/service -run 'Test(MultiKnowledge|MultiSearch|Multi.*Rerank)' -count=1  
Expected: PASS。

- [x] Step 4: 提交

    git add internal/application/service/multi_knowledge_search.go internal/application/service/multi_knowledge_search_test.go internal/application/service/search_rerank.go internal/application/service/multi_knowledge_rerank_test.go
    git commit -m "feat(search): 记录多知识库检索范围"

### Task 6: 实现固定快照管理员回放

**Files:**
- Create: internal/application/service/search_replay.go
- Create: internal/application/service/search_replay_test.go
- Modify: internal/application/service/search.go
- Modify: internal/application/service/multi_knowledge_search.go
- Modify: internal/ports/index/index.go
- Modify: internal/infrastructure/db/retrieval_search_repository.go
- Modify: internal/domain/errors/errors.go

**Interfaces:**
- Produces: SearchReplayService.Replay。
- Produces: package 内部 SearchSnapshotOverride。
- Adds: SearchReader.GetGeneration。
- Adds: ErrGenerationNotAvailable、ErrSearchQueryMismatch。

- [x] Step 1: 写回放拒绝测试

    func TestSearchReplayRejectsDifferentQuery(t *testing.T) {
        service := replayServiceWithRun(originalRun("退款政策"))
        _, err := service.Replay(context.Background(), ReplaySearchInput{
            WorkspaceID: workspaceID,
            SearchRunID: originalRunID,
            Query: "安装指南",
            ActorRole: value.WorkspaceRoleAdmin,
        })
        require.ErrorIs(t, err, domainerrors.ErrSearchQueryMismatch)
    }

    func TestSearchReplayRejectsBearer(t *testing.T) {
        _, err := service.Replay(context.Background(), ReplaySearchInput{
            WorkspaceID: workspaceID,
            SearchRunID: originalRunID,
            Query: "退款政策",
            IsAPIKey: true,
        })
        require.ErrorIs(t, err, domainerrors.ErrForbidden)
    }

Run: go test ./internal/application/service -run TestSearchReplay -count=1  
Expected: FAIL。

- [x] Step 2: 提取固定快照搜索核心

普通 SearchInput 不暴露 generation_id。ReplayService 从 SearchRun 构造 SearchSnapshotOverride，包含 KB 到 Generation snapshot 的映射、Rerank snapshot、ReplayOfID 和原 topK。固定路径校验 workspace/KB/generation、config/model hash 和 published projection；不执行 active pointer CAS；不可用时返回 ErrGenerationNotAvailable。

- [x] Step 3: 实现 ReplayService

    func (s *SearchReplayService) Replay(ctx context.Context, input ReplaySearchInput) (*dto.SearchResponse, error) {
        if input.IsAPIKey ||
            (input.ActorRole != value.WorkspaceRoleOwner &&
                input.ActorRole != value.WorkspaceRoleAdmin) {
            return nil, domainerrors.ErrForbidden
        }
        run, err := s.runs.Get(ctx, input.WorkspaceID, input.SearchRunID)
        if err != nil {
            return nil, err
        }
        if searchQueryHash(input.Query) != run.QueryHash {
            return nil, domainerrors.ErrSearchQueryMismatch
        }
        return s.executeSnapshot(ctx, run, input.Query)
    }

回放创建新 SearchRun，并设置 ReplayOfID。快照 mismatch 不允许 fallback 到 active Generation。

- [x] Step 4: 运行回放测试

增加单库、多库、Generation projection 缺失、model hash mismatch、当前权限不足测试。

Run: go test ./internal/application/service ./internal/ports/index -run 'Test(SearchReplay|Search.*Snapshot|Multi.*Snapshot)' -count=1  
Expected: PASS。

- [x] Step 5: 提交

    git add internal/application/service/search_replay.go internal/application/service/search_replay_test.go internal/application/service/search.go internal/application/service/multi_knowledge_search.go internal/ports/index/index.go internal/infrastructure/db/retrieval_search_repository.go internal/domain/errors/errors.go
    git commit -m "feat(search): 支持管理员固定快照回放"

### Task 7: 接入 REST、MCP 和 OpenAPI

**Files:**
- Modify: internal/interfaces/http/search_handler.go
- Modify: internal/interfaces/http/search_handler_test.go
- Create: internal/interfaces/http/search_replay_handler.go
- Create: internal/interfaces/http/search_replay_handler_test.go
- Modify: internal/interfaces/http/router.go
- Modify: internal/interfaces/http/openapi_routes.go
- Modify: internal/interfaces/http/openapi_test.go
- Modify: internal/interfaces/http/errors.go
- Modify: internal/interfaces/mcp/tools.go
- Modify: internal/interfaces/mcp/server_test.go

**Interfaces:**
- Produces: 单库 X-Search-ID、X-Retrieval-Status、X-Generation-IDs。
- Produces: 多库/MCP wrapper 新字段。
- Produces: owner/admin replay route。

- [x] Step 1: 写单库兼容测试

    func TestSingleSearchKeepsArrayBodyAndAddsHeaders(t *testing.T) {
        response := testSearchResponse()
        recorder := serveSingleSearch(response)
        require.JSONEq(t, marshalJSON(response.Results), recorder.Body.String())
        require.Equal(t, response.Run.SearchID.String(),
            recorder.Header().Get("X-Search-ID"))
    }

Run: go test ./internal/interfaces/http -run TestSingleSearchKeepsArrayBodyAndAddsHeaders -count=1  
Expected: FAIL。

- [x] Step 2: 改造 REST handler

单库写三个 Header 后返回 response.Results。调用 service 后即使 err 非空，只要 response 非空也必须先写 SearchRun Headers，再调用 writeServiceError。多库 wrapper 增加 search_id、requested_scope、effective_scope、retrieval_status、generation_ids，同时保留 searched_knowledge_base_ids/results。

- [x] Step 3: 实现 replay handler

POST /api/v1/workspaces/:workspace_slug/search-runs/:search_id/replay 只接收 query。Router 放入 Session owner/admin group；Bearer API Key 返回 403。ErrSearchQueryMismatch 映射 409 search_query_mismatch；ErrGenerationNotAvailable 映射 409 generation_not_available。

- [x] Step 4: 更新 MCP

knowledge_search 空 IDs 时展开 auth.KnowledgeBaseIDs 并设置 api_key_bound_all；显式 IDs 设置 selected。Output 增加 search_id、scope、retrieval_status、generation_ids，原字段保持。SearchRun 创建后的错误使用新的 toSearchErrorResult，把 search_id 和 failure_class 放入 isError=true 的稳定错误对象；创建前错误继续使用 toErrorResult。

- [x] Step 5: 更新 OpenAPI 和 adapter 测试

声明单库三个响应头、更新多库 schema、新增 replay route。MCP schema 测试继续断言 UUID 推导为 JSON string。

Run: go test ./internal/interfaces/http ./internal/interfaces/mcp -run 'Test.*(Search|Replay|OpenAPI|KnowledgeSearch)' -count=1  
Expected: PASS。

- [x] Step 6: 提交

    git add internal/interfaces/http/search_handler.go internal/interfaces/http/search_handler_test.go internal/interfaces/http/search_replay_handler.go internal/interfaces/http/search_replay_handler_test.go internal/interfaces/http/router.go internal/interfaces/http/openapi_routes.go internal/interfaces/http/openapi_test.go internal/interfaces/http/errors.go internal/interfaces/mcp/tools.go internal/interfaces/mcp/server_test.go
    git commit -m "feat(api): 暴露检索运行与回放合同"

### Task 8: 接入保留期清理、DI、文档和 E2E

**Files:**
- Modify: internal/infrastructure/config/config.go
- Modify: internal/infrastructure/config/config_test.go
- Modify: config.example.yaml
- Create: internal/application/service/search_run_cleanup.go
- Create: internal/application/service/search_run_cleanup_test.go
- Modify: cmd/langhuan/main.go
- Modify: cmd/langhuan/main_test.go
- Create: cmd/langhuan/v090_e2e_test.go
- Modify: docs/ARCHITECTURE.md
- Modify: docs/API_ACCESS.md
- Modify: docs/DATABASE_GUIDELINES.md
- Modify: ROADMAP.md

**Interfaces:**
- Produces: RetrievalConfig.SearchRunRetention，默认 168h。
- Produces: SearchRunCleanupService.Run。
- Wires: Repository 到单库、多库、Replay、cleanup scheduler 和 router。

- [x] Step 1: 写配置与 cleanup 测试

    func TestSearchRunRetentionDefault(t *testing.T) {
        cfg := Default()
        require.Equal(t, 168*time.Hour, cfg.Retrieval.SearchRunRetention)
    }

    func TestSearchRunCleanupUsesBatchLimit(t *testing.T) {
        store := &fakeSearchRunCleanupStore{deleted: 23}
        count, err := NewSearchRunCleanupService(store).
            Run(context.Background(), fixedNow, 1000)
        require.NoError(t, err)
        require.EqualValues(t, 23, count)
        require.Equal(t, 1000, store.limit)
    }

Run: go test ./internal/infrastructure/config ./internal/application/service -run 'TestSearchRun(Retention|Cleanup)' -count=1  
Expected: FAIL。

- [x] Step 2: 实现配置、cleanup 和 DI

config.example.yaml 增加 search_run_retention: 168h。配置校验拒绝 search_run_retention 大于 retired_generation_retention。cleanup scheduler 复用进程 context 和现有 cleanup interval，先清理 SearchRun 再清理 retired projection；不得启动不可取消 goroutine。main.go 创建 SearchRunRepository，并注入单库、多库、ReplayService、HTTP 和 cleanup。

- [x] Step 3: 写 v0.9 E2E

    func TestV090SearchRunAndReplay(t *testing.T) {
        env := newE2EEnvironment(t)
        seedReadyKnowledge(t, env)
        first := searchAndReadRunHeaders(t, env, "退款政策")
        require.NotEmpty(t, first.SearchID)
        require.NotEmpty(t, first.Results[0].DocumentRevisionID)
        require.NotEmpty(t, first.Results[0].IndexGenerationID)
        require.Len(t, first.Results[0].Citation.ContentSHA256, 64)

        replayed := replaySearchAsAdmin(t, env, first.SearchID, "退款政策")
        require.NotEqual(t, first.SearchID, replayed.Run.SearchID)
        require.NotNil(t, replayed.Run.ReplayOfID)
        require.Equal(t, first.SearchID, *replayed.Run.ReplayOfID)
    }

同文件覆盖 API Key replay 403、不同 query 409、MCP all-bound scope、单库数组兼容和数据库不含 query 明文。

- [x] Step 4: 更新文档

ARCHITECTURE 增加 SearchRun/CitationRef 数据流；API_ACCESS 记录响应头、MCP wrapper、分数语义和 replay 权限；DATABASE_GUIDELINES 记录新表和复合外键；ROADMAP 增加 v0.9.0 验收标准。

- [x] Step 5: 完整验证

Run: gofmt -w internal cmd  
Expected: 无输出。

Run: go test ./...  
Expected: PASS。

Run: make test-integration  
Expected: PASS，临时容器在测试后销毁。

Run: go vet ./...  
Expected: PASS。

Run: git diff --check  
Expected: 无输出。

- [x] Step 6: 提交

    git add internal/infrastructure/config/config.go internal/infrastructure/config/config_test.go config.example.yaml internal/application/service/search_run_cleanup.go internal/application/service/search_run_cleanup_test.go cmd/langhuan/main.go cmd/langhuan/main_test.go cmd/langhuan/v090_e2e_test.go docs/ARCHITECTURE.md docs/API_ACCESS.md docs/DATABASE_GUIDELINES.md ROADMAP.md
    git commit -m "feat(search): 完成检索运行保留和端到端接入"

## 实施完成后的评审检查

- SearchResult 新字段来自 retrieval projection 真实 lineage。
- 单库 body 仍是数组；MCP 空 scope 只展开当前 API Key 绑定集合。
- SearchRun、日志和 Trace 均没有 query 原文、正文、向量或凭证。
- 回放使用原 Generation/topK/Rerank snapshot，快照不匹配时失败。
- SearchRun persistence failure 不改变原检索结果或原领域错误。
- retention cleanup 不删除 Generation、Revision 或 retrieval entries。
- 规格中的每条验收标准都能对应到 Task 1–8 的测试。

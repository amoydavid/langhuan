# Jinshu 管理面程序化 API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 在 langhuan 中为 jinshu 管理面提供带完整 Workspace/KnowledgeBase lineage 的 Bearer API，支持受限管理、Markdown 文本导入、文档类型过滤和 Embedding 模型选择。

**Architecture:** 复用现有 Gin handler、application service、DTO、Repository 和异步导入流水线。路由层通过 SessionOrAPIKeyAuth、scope middleware 和 KnowledgeBase binding 做快速拒绝；service 使用显式 WorkspaceID、KnowledgeBaseID 与 ResourceAccess 做最终授权；Repository 只执行带完整租户条件的查询。

**Tech Stack:** Go 1.26、Gin、GORM/PostgreSQL、pgvector/zhparser、asynq、Redis、testcontainers-go、golang-migrate、testing、testify。

## Global Constraints

- 所有开放 URL 必须包含 /workspaces/:workspace_slug；KnowledgeBase 子资源必须包含 /knowledge-bases/:id，FAQ get/update 使用新的带 KB 路径。
- 领域层不依赖 HTTP、GORM 或第三方 SDK；application 通过使用方定义的 Repository/Store 接口访问数据库。
- Repository 查询必须显式带 workspace_id 和相应的 knowledge_base_id，事务内部始终使用传入的 tx。
- Bearer 越界统一返回 404 not_found；缺 scope 返回 403 insufficient_scope；无效 Bearer 返回 401 unauthorized。
- API Key 不继承创建者 role；知识库更新由 service 明确区分 Session admin 与 API Key knowledge_bases:write。
- 文本导入只支持 Markdown，使用 ingest.max_file_size_bytes，必须复用 DocumentIngestService.Ingest，HTTP 不等待解析完成。
- 数据库集成/E2E 测试只能使用测试运行期临时 Docker PostgreSQL（pgvector + zhparser）和 Redis；未设置 LANGHUAN_TEST_DATABASE_DSN 时不得回退到 config.yaml。
- 不新增数据库迁移，不实现 KnowledgeBase 删除、Chunk 写、Generation 写或 search-settings 写。
- 每条对外 REST 路由必须同步维护 internal/interfaces/http/openapi_routes.go；OpenAPI 的 security、required scope extension、path/query 参数、request/response schema 与 Gin 路由保持一致。

---

## 文件变更总览

| 文件 | 职责 |
|---|---|
| internal/domain/value/api_scope.go | 新增合法 scope |
| internal/application/service/knowledge_base.go、knowledge_base_model_binder.go、model_repository.go | KB 列表访问过滤与 Bearer 更新授权 |
| internal/application/service/document.go、document_ingest.go | kind 过滤和文本导入复用 |
| internal/application/service/faq_document.go、file_tree.go、knowledge_base_summary.go、document_chunks.go | 完整 workspace/KB lineage 与 access 校验 |
| internal/application/service/model.go | Bearer 模型 selectable 约束 |
| internal/infrastructure/db/knowledge_base_repository.go、document_repository.go | allowed KB 与 kind 条件下推 |
| internal/interfaces/http/router.go | progGroup 路由与 scope 注册 |
| internal/interfaces/http/knowledge_base.go、document.go、faq_document_handler.go、model_handler.go | 请求解析、主体分流、DTO 响应 |
| internal/interfaces/http/file_tree_handler.go、knowledge_base_summary_handler.go、document_chunks_handler.go | KB 子资源入口统一 |
| internal/interfaces/http/*_test.go | handler/service 合同测试 |
| internal/interfaces/http/openapi_routes.go、openapi_spec.go | 对外路由 operation、参数、scope extension 与反射 schema |
| internal/interfaces/http/openapi_test.go | OpenAPI path/security/parameter/schema 一致性测试 |
| cmd/langhuan/jinshu_management_api_e2e_test.go | 真实 HTTP/worker/数据库/Redis E2E |
| docs/API_ACCESS.md | 程序化 API 使用合同 |

---

### Task 1: 新增 scope 并锁定 URL/API 合同

Files:
- Modify: internal/domain/value/api_scope.go
- Modify: internal/interfaces/http/router.go
- Test: internal/domain/value/model_value_test.go
- Test: internal/interfaces/http/api_key_middleware_test.go

Interfaces:
- Produces value.ScopeKnowledgeBasesRead APIScope = "knowledge_bases:read".
- Produces progGroup routes using RequireScopeForAPIKey and RequireKnowledgeBaseForAPIKey("id").

- [ ] Step 1: Add the scope value and table-driven validity tests. Assert the new scope is valid, appears once in AllAPIScopes, and existing scopes remain valid.
- [ ] Step 2: Register KB list/get/patch, summary/jobs, document list, file-tree, KB-qualified FAQ, chunks, text ingest and models. Leave Generation, Chunk revision write and search-settings write Session-only.
- [ ] Step 3: Add middleware tests for 403 insufficient_scope, 404 not_found, and Session pass-through.
- [ ] Step 4: Run: go test ./internal/domain/value ./internal/interfaces/http -run 'APIScope|APIKey|Router' -count=1. Expected: PASS.
- [ ] Step 5: Commit with: feat(http): 开放 jinshu 程序化路由 scope.

### Task 2: 统一 KnowledgeBase access 合同

Files:
- Modify: internal/application/service/knowledge_base.go
- Modify: internal/application/service/knowledge_base_model_binder.go
- Modify: internal/application/service/model_repository.go
- Modify: internal/infrastructure/db/knowledge_base_repository.go
- Modify: internal/interfaces/http/knowledge_base.go
- Test: internal/application/service/knowledge_base_test.go
- Test: internal/interfaces/http/knowledge_base_test.go

Interfaces:
- KnowledgeBaseListFilter has WorkspaceID and AllowedKnowledgeBaseIDs.
- UpdateKnowledgeBaseBasicsInput has WorkspaceID, KnowledgeBaseID, Name, Description, ActorRole, Access and IsAPIKey.

- [ ] Step 1: Write table-driven tests for unrestricted Session list, API Key list with two allowed IDs, empty/non-matching binding, cross-workspace get, Session member forbidden patch and API Key bound patch.
- [ ] Step 2: Pass exact allowed UUIDs to binder/repository; do not fetch all rows and filter after DTO conversion.
- [ ] Step 3: Require workspace equality and Access.AllowsKnowledgeBase for API Key patch; Session continues to require admin role.
- [ ] Step 4: Push allowed IDs into GORM with workspace_id = ? AND id IN ? and preserve resolved model joins/error mapping.
- [ ] Step 5: Populate workspace, KB ID and access from AuthContext; keep DTO fields unchanged.
- [ ] Step 6: Run go test ./internal/application/service ./internal/interfaces/http -run 'KnowledgeBase' -count=1. Expected: PASS.
- [ ] Step 7: Commit with: feat(auth): 统一知识库 workspace 与绑定范围校验.

### Task 3: 文档 kind 过滤

Files:
- Modify: internal/application/service/document.go
- Modify: internal/infrastructure/db/document_repository.go
- Modify: internal/interfaces/http/document.go
- Test: internal/application/service/document_test.go
- Test: internal/interfaces/http/router_test.go
- Test: internal/infrastructure/db/document_repository_integration_test.go

Interfaces:
- DocumentListFilter has WorkspaceID, KnowledgeBaseID and optional Kind.
- DocumentQueryService.List accepts DocumentListFilter; Get/Delete retain ResourceAccess.

- [ ] Step 1: Add tests for file, faq, web, nil and invalid kind; verify workspace and KB lineage.
- [ ] Step 2: Parse only kind=file|faq|web; absent means nil; invalid values return 400 validation_error before service invocation.
- [ ] Step 3: Add repository kind predicate while retaining deleted_at IS NULL, ordering and active revision loading.
- [ ] Step 4: Update route registration and fakes.
- [ ] Step 5: Run go test ./internal/application/service ./internal/interfaces/http -run 'Document.*List|Kind' -count=1. Expected: PASS.
- [ ] Step 6: Run make test-image && go test -tags=integration ./internal/infrastructure/db -run 'DocumentRepository.*Kind|Document.*List' -count=1. Expected: PASS against langhuan-test-postgres:pg17 or explicit skip when Docker is unavailable.
- [ ] Step 7: Commit with: feat(document): 支持按 kind 过滤文档列表.

### Task 4: Markdown 文本导入

Files:
- Modify: internal/interfaces/http/document.go
- Modify: internal/interfaces/http/router.go
- Modify: internal/application/service/document_ingest.go only if shared validation needs a named limit helper
- Create: internal/interfaces/http/document_text_handler_test.go
- Test: internal/application/service/document_ingest_test.go

Interfaces:
- createTextDocumentRequest has Title, Content, ContentType and ParentNodeID.
- Handler calls existing Ingest with FileName=title+".md", ContentType=text/markdown, SourceType=api, Reader=strings.NewReader(content), Dedupe=false and NodeName=title.

- [ ] Step 1: Write tests for strict JSON, title/content required, markdown only, byte limit, parent UUID, exact ingest input and 201 result.
- [ ] Step 2: Implement createText using len([]byte(content)), configured max, strings.NewReader and request context.
- [ ] Step 3: Add service ingest test confirming SourceType=api and Markdown use the existing parser/queue path.
- [ ] Step 4: Register POST /knowledge-bases/:id/documents/text with documents:write and KB binding.
- [ ] Step 5: Run go test ./internal/interfaces/http ./internal/application/service -run 'Text|Ingest' -count=1. Expected: PASS.
- [ ] Step 6: Commit with: feat(document): 增加 Markdown 文本导入接口.

### Task 5: FAQ、FileTree、Summary、Chunks 的完整 lineage

Files:
- Modify: internal/application/service/faq_document.go
- Modify: internal/interfaces/http/faq_document_handler.go
- Modify: internal/application/service/file_tree.go
- Modify: internal/application/service/knowledge_base_summary.go
- Modify: internal/interfaces/http/knowledge_base_summary_handler.go
- Modify: internal/application/service/document_chunks.go
- Modify: internal/interfaces/http/document_chunks_handler.go
- Modify: internal/interfaces/http/router.go
- Test: internal/application/service/faq_document_test.go
- Test: internal/application/service/file_tree_test.go
- Test: internal/application/service/knowledge_base_summary_test.go
- Test: internal/application/service/document_chunks_test.go
- Test: internal/interfaces/http/faq_document_handler_test.go

Interfaces:
- FAQ Get receives WorkspaceID + KnowledgeBaseID + DocumentID; Update validates all three plus BaseRevisionID.
- FileTree/Summary/Chunks retain explicit workspace + KB IDs and receive ResourceAccess where a restricted principal can reach the entry point.

- [ ] Step 1: Add tests for other-KB, other-workspace and non-FAQ ErrNotFound plus stale revision ErrRevisionConflict.
- [ ] Step 2: Parse id and document_id, set CreatedBy=nil for API Key, keep Session user ID, and remove old FAQ URL.
- [ ] Step 3: Check Access.WorkspaceID, AllowsKnowledgeBase and explicit KB ID before transactions.
- [ ] Step 4: Run go test ./internal/application/service ./internal/interfaces/http -run 'FAQ|FileTree|Summary|DocumentChunks' -count=1. Expected: PASS.
- [ ] Step 5: Commit with: feat(auth): 为知识库子资源补齐 workspace 与 KB lineage.

### Task 6: Bearer 模型列表合同

Files:
- Modify: internal/application/service/model.go
- Modify: internal/interfaces/http/model_handler.go
- Modify: internal/interfaces/http/router.go
- Create: internal/application/service/model_selectable_test.go
- Test: internal/interfaces/http/model_routes_test.go

Interfaces:
- SelectableModelFilter has Type, Status and Scope.
- ListSelectableForAPIKey(context.Context, uuid.UUID, SelectableModelFilter) returns []*dto.Model.

- [ ] Step 1: Test rerank, disabled, workspace-owned, management and all-type rejection.
- [ ] Step 2: Query existing visible models with server-enforced embedding/active/platform; never expose Provider config/credentials.
- [ ] Step 3: For API Key require exactly type=embedding, status=active and scope=platform; retain Session behavior.
- [ ] Step 4: Register /models in progGroup with knowledge_bases:write.
- [ ] Step 5: Run go test ./internal/application/service ./internal/interfaces/http -run 'Model.*Selectable|Model.*List' -count=1. Expected: PASS.
- [ ] Step 6: Commit with: feat(model): 开放受限 Bearer Embedding 模型列表.

### Task 7: OpenAPI 声明与一致性测试

Files:
- Modify: internal/interfaces/http/openapi_routes.go
- Modify: internal/interfaces/http/openapi_spec.go
- Modify: internal/interfaces/http/openapi_test.go

Interfaces:
- Extend the hand-written op metadata with path/query parameters and required scopes, while keeping field schemas generated from Go structs.
- Keep secBearerOrSession for every Session/Bearer route and expose required scopes through operation extension x-langhuan-required-scopes.

- [ ] Step 1: Add a route metadata type for parameters. It must represent name, in=path/query, required, scalar schema type/format, enum values and description; path parameters are required, kind/type/status/scope enums are explicit.
- [ ] Step 2: Add required scope metadata and attach x-langhuan-required-scopes in specBuilder.add. Add knowledge_bases:read to schemaCustomizer's APIScope enum.
- [ ] Step 3: Update knowledgeBaseOps, documentOps, faqOps, fileTreeOps, chunkOps, jobOps and modelOps. Use the KB-qualified FAQ paths; declare text request/response; declare kind, enabled/cursor/limit, document_id/status/cursor/limit and model type/status/scope parameters.
- [ ] Step 4: Add tests in openapi_test.go for every new/migrated path: operation exists, old FAQ path is absent, security has the Session/Bearer OR pair, required scope extension is exact, path parameters are required, query enums/defaults are present, and response/request schemas resolve to the expected DTO shapes.
- [ ] Step 5: Run go test ./internal/interfaces/http -run 'OpenAPI|Spec' -count=1. Expected: PASS and kin-openapi loader accepts the generated document.
- [ ] Step 6: Commit with: feat(openapi): 声明 jinshu 程序化接口合同.

### Task 8: HTTP route matrix and error-contract tests

Files:
- Modify: internal/interfaces/http/router_test.go
- Modify: internal/interfaces/http/model_routes_test.go
- Modify: internal/interfaces/http/faq_document_handler_test.go
- Create: internal/interfaces/http/jinshu_management_routes_test.go

- [ ] Step 1: Add a table of method, path, scope, principal and expected status for every endpoint, valid Session/Bearer, missing scope, malformed UUID and unbound KB.
- [ ] Step 2: Decode stable error bodies as {"error":{"code":"..."}}; never compare SQL or middleware messages.
- [ ] Step 3: Assert text ingest document/job/deduped, FAQ document/revision/questions/answer/job and model DTO mappings.
- [ ] Step 4: Run go test ./internal/interfaces/http -count=1. Expected: PASS.
- [ ] Step 5: Commit with: test(http): 覆盖 jinshu Bearer 路由与错误合同.

### Task 9: 完整 HTTP/Worker/Database/Redis E2E（必须新增）

Files:
- Create: cmd/langhuan/jinshu_management_api_e2e_test.go with //go:build integration
- Modify: cmd/langhuan/postgres_testmain_integration_test.go only if shared TestMain needs a setup hook
- Reuse: cmd/langhuan/v030_e2e_test.go, internal/testsupport/postgres.go and internal/testsupport/redis.go

Interfaces:
- Use the real cmd/langhuan route assembly and worker services.
- PostgreSQL uses testsupport.RunPostgresTestMain(m, migrate.Run) or externally injected disposable LANGHUAN_TEST_DATABASE_DSN; Redis uses testsupport.NewIsolatedRedis(t).

- [ ] Step 1: Add the integration file with no localhost/config fallback.
- [ ] Step 2: Seed one workspace with two KBs and an active platform Embedding model; create a key bound to one KB with all four read/write scopes.
- [ ] Step 3: Verify KB access: list returns exactly the bound KB; get/summary/list/file-tree/jobs/chunks on the unbound KB return 404; bound KB patch succeeds.
- [ ] Step 4: Verify text ingest: POST Markdown, assert 201 and non-empty document/job IDs, poll the real Job to a terminal state, and assert document kind/title plus file-tree node.
- [ ] Step 5: Verify kind and FAQ: create FAQ through the KB-qualified URL, get/update with returned revision, assert kind=faq, and assert mismatched KB/document URL is 404.
- [ ] Step 6: Verify file-tree and model operations: create/rename/delete empty folder, assert non-empty deletion 409, and verify only type=embedding&status=active&scope=platform succeeds.
- [ ] Step 7: Verify auth precedence and revocation: invalid Bearer plus valid Cookie returns 401; revoked key returns 401 for programmatic endpoints.
- [ ] Step 8: Run make test-image && go test -tags=integration ./cmd/langhuan -run 'JinshuManagementAPI' -count=1. Expected: PASS against disposable PostgreSQL + Redis.
- [ ] Step 9: Commit with: test(e2e): 覆盖 jinshu 管理面程序化 API 全链路.

### Task 10: 更新程序化访问文档

Files:
- Modify: docs/API_ACCESS.md
- Modify: docs/ARCHITECTURE.md only where the public REST capability table needs the new Bearer surface

- [ ] Step 1: Document the new scope and complete route table, including exact FAQ paths, text request/response, kind values, model constraints and errors.
- [ ] Step 2: Add security notes for API Key binding, invalid Bearer precedence and server-enforced list filtering.
- [ ] Step 3: Cross-check every documented method/path/scope against route registration and a test row.
- [ ] Step 4: Commit with: docs(api): 补充 jinshu 程序化管理接口合同.

### Task 11: 全量验证与交付检查

Files:
- Modify: none unless a preceding test or documentation check identifies a concrete defect.

- [ ] Step 1: Run gofmt -w internal cmd, go vet ./... and git diff --check. Expected: exit 0.
- [ ] Step 2: Run go test ./... -count=1. Expected: PASS.
- [ ] Step 3: Run make test-integration. Expected: it builds langhuan-test-postgres:pg17, injects LANGHUAN_TEST_DATABASE_DSN, starts disposable PostgreSQL, and all integration tests including TestJinshuManagementAPI pass; Redis is cleaned up by test support.
- [ ] Step 4: Run git status --short, git diff --stat and rg -n 'TODO|TBD' on both docs. Expected: no placeholders.
- [ ] Step 5: Commit any final verification-only changes with: chore: 完成 jinshu 管理 API 规格验证.

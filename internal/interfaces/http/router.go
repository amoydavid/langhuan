package http

import (
	"io/fs"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// Dependencies wires all HTTP handler dependencies. Auth services + session
// config drive the auth/invitation/membership/user handlers; the resource
// services drive the workspace-scoped handlers.
type Dependencies struct {
	// auth
	Auth            AuthService               // register/login/logout/me/authenticate (SessionAuthenticator)
	Users           UserService               // first-user register, password reset, get-by-id
	Invitations     InvitationService         // create/get-public/accept/revoke
	Memberships     MembershipService         // list/get/change-role/remove/list-for-user
	SessionConfig   config.SessionConfig      // cookie name, lifetime, secure, domain
	PublicURLs      *service.PublicURLBuilder // 全局公开地址派生器
	APIKeys         APIKeyServiceHTTP         // Session-only API Key 管理
	APIKeyAuth      APIKeyAuthenticator       // Bearer API Key 鉴权器（SessionOrAPIKeyAuth / APIKeyOnlyAuth 共用）
	OIDC            OIDCLoginServiceHTTP      // OIDC 登录/绑定/查询；nil 时不挂 OIDC 路由
	OIDCAcceptor    OIDCAcceptor              // AcceptOIDC 邀请接受；nil 时该分派路径不可用
	OIDCCompleter   OIDCInvitationCompleter   // 补齐 email 后完成邀请接受；nil 时该路径不可用
	OIDCEnabled     bool                      // bootstrap-status 返回，控制前端是否显示 OIDC 入口
	PasswordEnabled bool                      // bootstrap-status 返回，控制前端密码表单显示

	// resource (workspace-scoped)
	Workspaces              WorkspaceService
	WorkspaceReadiness      WorkspaceReadinessHTTPService
	WorkspaceSearchSettings WorkspaceSearchSettingsHTTPService
	KnowledgeBases          KnowledgeBaseService
	KnowledgeBaseSync       KnowledgeBaseSyncService
	KnowledgeBaseSummary    KnowledgeBaseSummaryHTTPService
	DocumentChunks          DocumentChunksHTTPService
	ModelProviders          ModelProviderHTTPService
	Models                  ModelHTTPService
	ModelConnectionTests    ModelConnectionTestHTTPService
	DocumentIngest          DocumentIngestService
	Documents               DocumentQueryService
	DocumentAssets          DocumentAssetListService
	AssetGetter             DocumentAssetGetter
	AssetContentStore       AssetContentStore
	FAQDocuments            FAQDocumentHTTPService
	FileTree                FileTreeHTTPService
	ChunkRevisions          ChunkRevisionHTTPService
	IndexGenerations        IndexGenerationHTTPService
	Search                  SearchHTTPService
	MultiSearch             MultiSearchHTTPService
	Jobs                    JobQueryService
	SourceConnections       SourceConnectionService
	MCPHandler              stdhttp.Handler
	SPA                     fs.FS
	MaxFileSizeBytes        int64
}

// NewRouter builds the gin engine wiring:
//   - public REST: /api/v1/healthz, /api/v1/auth/register,
//     /api/v1/auth/login, GET /api/v1/invitations/:token
//   - authenticated (SessionAuth): /api/v1/auth/logout, /api/v1/auth/me
//   - platform admin (SessionAuth + RequirePlatformAdmin):
//     POST /api/v1/workspaces, DELETE /api/v1/invitations/:invitation_id,
//     POST /api/v1/admin/users/:user_id/password-reset
//   - workspace-scoped (SessionAuth + RequireWorkspace + RequireWorkspaceRole):
//     /api/v1/workspaces/:workspace_slug/...
//   - MCP: /mcp and /mcp/*path, outside the REST namespace
//
// healthz and /mcp stay auth-free.
func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(RequestID())

	cookieName := deps.SessionConfig.CookieName
	api := router.Group("/api/v1")

	// --- healthz + MCP ---
	api.GET("/healthz", healthz)

	// --- OpenAPI 文档端点（登录后可见）---
	// spec 在启动时反射 struct 生成一次；UI 与 spec JSON 挂在 SessionAuth 之后，
	// 已登录用户才能访问，接口结构不向未认证者暴露。
	api.Group("", SessionAuth(deps.Auth, cookieName)).GET("/openapi.json", serveOpenAPISpec(buildSpec()))
	api.Group("", SessionAuth(deps.Auth, cookieName)).GET("/docs", serveDocsUI())

	if deps.MCPHandler != nil {
		// /mcp 只接受 Bearer API Key，不接受浏览器 Cookie，不进入 SPA fallback。
		mcpAuth := router.Group("/mcp")
		mcpAuth.Use(MCPTransport())
		if deps.APIKeyAuth != nil {
			mcpAuth.Use(APIKeyOnlyAuth(deps.APIKeyAuth))
		}
		mcpAuth.Any("", gin.WrapH(deps.MCPHandler))
		mcpAuth.Any("/*path", gin.WrapH(deps.MCPHandler))
	}

	// --- public auth + invitation routes ---
	if deps.Auth != nil || deps.Users != nil || deps.Invitations != nil {
		authH := authHandler{
			auth:            deps.Auth,
			users:           deps.Users,
			invitations:     deps.Invitations,
			memberships:     deps.Memberships,
			workspaces:      deps.Workspaces,
			sessionCfg:      deps.SessionConfig,
			oidcEnabled:     deps.OIDCEnabled,
			passwordEnabled: deps.PasswordEnabled,
		}
		api.POST("/auth/login", authH.login)
		api.POST("/auth/register", authH.register)
		if deps.Users != nil {
			api.GET("/auth/bootstrap-status", authH.bootstrapStatus)
		}

		if deps.Invitations != nil {
			invH := invitationHandler{invitations: deps.Invitations, publicURLs: deps.PublicURLs}
			api.GET("/invitations/:token", invH.getPublic)
		}

		// OIDC 登录/回调（条件挂载）。
		if deps.OIDC != nil {
			oidcH := newOIDCHandler(deps.OIDC, deps.OIDCAcceptor, deps.OIDCCompleter, deps.Auth, deps.SessionConfig)
			api.GET("/auth/oidc/login", oidcH.begin)
			api.GET("/auth/oidc/callback", oidcH.callback)
		}
	}

	// --- authenticated (logged-in) routes ---
	if deps.Auth != nil {
		authH := authHandler{
			auth:            deps.Auth,
			users:           deps.Users,
			invitations:     deps.Invitations,
			memberships:     deps.Memberships,
			workspaces:      deps.Workspaces,
			sessionCfg:      deps.SessionConfig,
			oidcEnabled:     deps.OIDCEnabled,
			passwordEnabled: deps.PasswordEnabled,
		}
		authed := api.Group("")
		authed.Use(SessionAuth(deps.Auth, cookieName))
		{
			authed.POST("/auth/logout", authH.logout)
			authed.GET("/auth/me", authH.me)
			if deps.Users != nil {
				authed.POST("/auth/change-password", authH.changePassword)
				authed.PUT("/auth/profile", authH.updateProfile)
			}
			// OIDC 绑定发起 + 外部身份查询（条件挂载）。
			if deps.OIDC != nil {
				oidcH := newOIDCHandler(deps.OIDC, deps.OIDCAcceptor, deps.OIDCCompleter, deps.Auth, deps.SessionConfig)
				authed.POST("/auth/oidc/bind/start", oidcH.beginBind)
				authed.GET("/auth/external-identities", oidcH.listIdentities)
			}
		}
	}

	// --- platform-admin routes ---
	adminReady := deps.Auth != nil && (deps.Workspaces != nil || deps.Invitations != nil || deps.Users != nil ||
		deps.ModelProviders != nil || deps.Models != nil || deps.ModelConnectionTests != nil)
	if adminReady {
		admin := api.Group("")
		admin.Use(SessionAuth(deps.Auth, cookieName), RequirePlatformAdmin())
		if deps.Workspaces != nil {
			ws := workspaceHandler{service: deps.Workspaces}
			admin.POST("/workspaces", ws.create)
		}
		if deps.Invitations != nil {
			invH := invitationHandler{invitations: deps.Invitations, publicURLs: deps.PublicURLs}
			admin.DELETE("/invitations/:invitation_id", invH.revokeAny)
		}
		if deps.Users != nil {
			userH := userHandler{users: deps.Users}
			admin.POST("/admin/users/:user_id/password-reset", userH.resetPassword)
		}
		if deps.ModelProviders != nil {
			providerH := modelProviderHandler{service: deps.ModelProviders}
			admin.GET("/admin/model-providers", providerH.listPlatform)
			admin.GET("/admin/model-providers/options", providerH.options)
			admin.POST("/admin/model-providers", providerH.createPlatform)
			admin.GET("/admin/model-providers/:provider_id/model-catalog", providerH.catalogPlatform)
			admin.GET("/admin/model-providers/:provider_id", providerH.getPlatform)
			admin.PATCH("/admin/model-providers/:provider_id", providerH.updatePlatform)
			admin.DELETE("/admin/model-providers/:provider_id", providerH.deletePlatform)
		}
		if deps.Models != nil {
			modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
			admin.POST("/admin/model-providers/:provider_id/models", modelH.createPlatform)
			admin.GET("/admin/model-providers/:provider_id/models", modelH.listPlatform)
			admin.GET("/admin/models", modelH.listPlatformModels)
			admin.GET("/admin/models/:model_id", modelH.getPlatform)
			admin.PATCH("/admin/models/:model_id", modelH.updatePlatform)
			admin.DELETE("/admin/models/:model_id", modelH.deletePlatform)
		}
		if deps.ModelConnectionTests != nil {
			modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
			admin.POST("/admin/models/:model_id/test", modelH.testPlatform)
		}
	}

	// --- workspace-scoped routes ---
	if deps.Workspaces != nil && deps.Memberships != nil {
		ws := workspaceHandler{service: deps.Workspaces}
		wsGroup := api.Group("/workspaces/:workspace_slug")
		wsGroup.Use(
			SessionAuth(deps.Auth, cookieName),
			RequireWorkspace(deps.Workspaces, deps.Memberships),
		)

		// member+ routes (Session-only).
		memberGroup := wsGroup.Group("")
		memberGroup.Use(RequireWorkspaceRole(value.RoleMember))
		{
			memberGroup.GET("", ws.get)
			if deps.WorkspaceReadiness != nil {
				readiness := workspaceReadinessHandler{service: deps.WorkspaceReadiness}
				memberGroup.GET("/readiness", readiness.get)
			}
			if deps.WorkspaceSearchSettings != nil {
				settings := workspaceSearchSettingsHandler{service: deps.WorkspaceSearchSettings}
				memberGroup.GET("/search-settings", settings.get)
			}
			if deps.Memberships != nil {
				mbH := membershipHandler{memberships: deps.Memberships}
				memberGroup.GET("/members", mbH.list)
			}
			if deps.KnowledgeBases != nil {
				// KB list/get are registered in progGroup so Session and Bearer share one contract.
			}
			if deps.KnowledgeBaseSummary != nil {
				// Summary/jobs are registered in progGroup with knowledge-bases:read.
			}
			if deps.ModelProviders != nil {
				providerH := modelProviderHandler{service: deps.ModelProviders}
				memberGroup.GET("/model-providers", providerH.listWorkspace)
				// options 必须注册在 :provider_id 之前，Gin 静态优先
				memberGroup.GET("/model-providers/options", providerH.options)
				memberGroup.GET("/model-providers/:provider_id/model-catalog", providerH.catalogWorkspace)
				memberGroup.GET("/model-providers/:provider_id", providerH.getWorkspace)
			}
			if deps.Models != nil {
				modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
				memberGroup.GET("/model-providers/:provider_id/models", modelH.listWorkspace)
				// /models 必须注册在 /models/:model_id 之前，Gin 静态优先
				memberGroup.GET("/models/:model_id", modelH.getWorkspace)
			}
			if deps.Documents != nil {
				doc := documentHandler{
					ingestService:     deps.DocumentIngest,
					queryService:      deps.Documents,
					assetService:      deps.DocumentAssets,
					assetGetter:       deps.AssetGetter,
					assetContentStore: deps.AssetContentStore,
					maxFileSizeBytes:  deps.MaxFileSizeBytes,
				}
				memberGroup.GET("/documents/:document_id/assets", doc.assets)
				memberGroup.GET("/documents/:document_id/assets/:asset_id", doc.assetContent)
			}
			if deps.FAQDocuments != nil {
				// FAQ routes are registered in progGroup with a KB-qualified URL.
			}
			if deps.FileTree != nil {
				// File-tree routes are registered in progGroup.
			}
			if deps.ChunkRevisions != nil {
				chunks := chunkRevisionHandler{service: deps.ChunkRevisions}
				memberGroup.GET("/knowledge-bases/:id/chunks/:chunk_id/revisions", chunks.list)
			}
			if deps.DocumentChunks != nil {
				// Document chunks list is registered in progGroup.
			}
			if deps.IndexGenerations != nil {
				generations := indexGenerationHandler{service: deps.IndexGenerations}
				memberGroup.GET("/knowledge-bases/:id/index-generations", generations.list)
			}
		}

		// 程序化可访问路由（Session 或 Bearer API Key）。
		// 管理面（成员、邀请、模型、设置、API Key 管理、Generation 写、Chunk
		// Revision 写）仍只在上方 Session-only 的 wsGroup/memberGroup/adminGroup 下。
		// 即使未配置 APIKeyAuth，这些路由仍注册并以 Session 形式工作。
		progGroup := api.Group("/workspaces/:workspace_slug")
		progGroup.Use(
			SessionOrAPIKeyAuth(deps.Auth, deps.APIKeyAuth, cookieName),
			RequireWorkspace(deps.Workspaces, deps.Memberships),
			RequireWorkspaceRole(value.RoleMember),
		)
		if deps.KnowledgeBases != nil {
			kb := knowledgeBaseHandler{service: deps.KnowledgeBases}
			create := progGroup.Group("/knowledge-bases", RequireScopeForAPIKey(value.ScopeKnowledgeBasesWrite))
			create.POST("", kb.create)
			read := progGroup.Group("/knowledge-bases", RequireScopeForAPIKey(value.ScopeKnowledgeBasesRead))
			read.GET("", kb.list)
			read.GET("/:id", kb.get)
			readSummary := progGroup.Group("/knowledge-bases/:id", RequireScopeForAPIKey(value.ScopeKnowledgeBasesRead), RequireKnowledgeBaseForAPIKey("id"))
			if deps.KnowledgeBaseSummary != nil {
				summary := knowledgeBaseSummaryHandler{service: deps.KnowledgeBaseSummary}
				readSummary.GET("/summary", summary.getSummary)
				jobs := progGroup.Group("/knowledge-bases/:id", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
				jobs.GET("/jobs", summary.listJobs)
			}
			write := progGroup.Group("/knowledge-bases/:id", RequireScopeForAPIKey(value.ScopeKnowledgeBasesWrite), RequireKnowledgeBaseForAPIKey("id"), RequireAdminForSession())
			write.PATCH("", kb.patch)
			if deps.KnowledgeBaseSync != nil {
				sync := knowledgeBaseSyncHandler{service: deps.KnowledgeBaseSync}
				write.POST("/sync", sync.sync)
			}
		} else if deps.KnowledgeBaseSummary != nil {
			readSummary := progGroup.Group("/knowledge-bases/:id", RequireScopeForAPIKey(value.ScopeKnowledgeBasesRead), RequireKnowledgeBaseForAPIKey("id"))
			summary := knowledgeBaseSummaryHandler{service: deps.KnowledgeBaseSummary}
			readSummary.GET("/summary", summary.getSummary)
			jobs := progGroup.Group("/knowledge-bases/:id", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			jobs.GET("/jobs", summary.listJobs)
		}
		if deps.Models != nil {
			modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
			models := progGroup.Group("", RequireScopeForAPIKey(value.ScopeKnowledgeBasesWrite))
			models.GET("/models", modelH.listSelectable)
		}
		if deps.Documents != nil {
			doc := documentHandler{ingestService: deps.DocumentIngest, queryService: deps.Documents, maxFileSizeBytes: deps.MaxFileSizeBytes}
			list := progGroup.Group("/knowledge-bases/:id/documents", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			list.GET("", doc.list)
			if deps.DocumentIngest != nil {
				write := progGroup.Group("/knowledge-bases/:id/documents", RequireScopeForAPIKey(value.ScopeDocumentsWrite), RequireKnowledgeBaseForAPIKey("id"))
				write.POST("", doc.ingest)
				write.POST("/text", doc.ingestText)
			}
		}
		if deps.FAQDocuments != nil {
			faq := faqDocumentHandler{service: deps.FAQDocuments}
			read := progGroup.Group("/knowledge-bases/:id/documents/:document_id/faq", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			read.GET("", faq.get)
			write := progGroup.Group("/knowledge-bases/:id/documents", RequireScopeForAPIKey(value.ScopeDocumentsWrite), RequireKnowledgeBaseForAPIKey("id"))
			write.POST("/faq", faq.create)
			write.PUT("/:document_id/faq", faq.update)
		}
		if deps.FileTree != nil {
			tree := fileTreeHandler{service: deps.FileTree}
			read := progGroup.Group("/knowledge-bases/:id/file-tree", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			read.GET("", tree.list)
			write := progGroup.Group("/knowledge-bases/:id/file-tree", RequireScopeForAPIKey(value.ScopeDocumentsWrite), RequireKnowledgeBaseForAPIKey("id"))
			write.POST("/folders", tree.createFolder)
			write.PATCH("/nodes/:node_id", tree.update)
			write.DELETE("/nodes/:node_id", tree.delete)
		}
		if deps.DocumentChunks != nil {
			chunks := documentChunksHandler{service: deps.DocumentChunks}
			read := progGroup.Group("/knowledge-bases/:id/documents/:document_id/chunks", RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			read.GET("", chunks.list)
		}
		// Document-only status/delete routes remain addressable by document ID. For
		// Bearer callers, the document service verifies that the document's
		// knowledge base belongs to the API key's allowed binding set.
		if deps.Documents != nil {
			doc := documentHandler{ingestService: deps.DocumentIngest, queryService: deps.Documents, maxFileSizeBytes: deps.MaxFileSizeBytes}
			status := progGroup.Group("", RequireScopeForAPIKey(value.ScopeDocumentsRead))
			status.GET("/documents/:document_id", doc.get)
			del := progGroup.Group("", RequireScopeForAPIKey(value.ScopeDocumentsWrite))
			del.DELETE("/documents/:document_id", doc.delete)
		}
		if deps.ChunkRevisions != nil {
			chunks := chunkRevisionHandler{service: deps.ChunkRevisions}
			chunk := progGroup.Group("/knowledge-bases/:id",
				RequireScopeForAPIKey(value.ScopeDocumentsRead), RequireKnowledgeBaseForAPIKey("id"))
			chunk.GET("/chunks/:chunk_id", chunks.get)
		}
		if deps.Search != nil {
			search := searchHandler{service: deps.Search, multiSearchSvc: deps.MultiSearch}
			searchGroup := progGroup.Group("/knowledge-bases/:id",
				RequireScopeForAPIKey(value.ScopeSearchRead), RequireKnowledgeBaseForAPIKey("id"))
			searchGroup.POST("/search", search.search)
			// 多知识库检索：POST /workspaces/:workspace_slug/search，知识库范围在服务层校验。
			if deps.MultiSearch != nil {
				multiGroup := progGroup.Group("", RequireScopeForAPIKey(value.ScopeSearchRead))
				multiGroup.POST("/search", search.multiSearchHandler)
			}
		}
		if deps.Jobs != nil {
			job := jobHandler{service: deps.Jobs}
			// Job status is open to both Session and Bearer callers with
			// documents:read. The service enforces the API-Key KB binding: an
			// unbound job maps to 404 (never leaks cross-tenant existence).
			// Session callers still pass (Unrestricted access).
			jobGroup := progGroup.Group("", RequireScopeForAPIKey(value.ScopeDocumentsRead))
			jobGroup.GET("/jobs/:id", job.get)
		}
		// API Key self-introspection: Bearer-only. Returns the authenticated
		// key's scope strings (no key value, no user data). Used by downstream
		// consumers (e.g. jinshu connection testing) to verify scope subsets.
		{
			self := progGroup.Group("/api-key", RequireAPIKeyOnly())
			self.GET("/self", apiKeySelfHandler{}.get)
		}

		if deps.ModelProviders != nil || deps.Models != nil || deps.ModelConnectionTests != nil || deps.WorkspaceSearchSettings != nil ||
			deps.KnowledgeBases != nil || deps.ChunkRevisions != nil || deps.IndexGenerations != nil {
			adminGroup := wsGroup.Group("")
			adminGroup.Use(RequireWorkspaceRole(value.RoleAdmin))
			if deps.ModelProviders != nil {
				providerH := modelProviderHandler{service: deps.ModelProviders}
				adminGroup.POST("/model-providers", providerH.createWorkspace)
				adminGroup.PATCH("/model-providers/:provider_id", providerH.updateWorkspace)
				adminGroup.DELETE("/model-providers/:provider_id", providerH.deleteWorkspace)
			}
			if deps.Models != nil {
				modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
				adminGroup.POST("/model-providers/:provider_id/models", modelH.createWorkspace)
				adminGroup.PATCH("/models/:model_id", modelH.updateWorkspace)
				adminGroup.DELETE("/models/:model_id", modelH.deleteWorkspace)
			}
			if deps.ModelConnectionTests != nil {
				modelH := modelHandler{models: deps.Models, connections: deps.ModelConnectionTests}
				adminGroup.POST("/models/:model_id/test", modelH.testWorkspace)
			}
			if deps.WorkspaceSearchSettings != nil {
				settings := workspaceSearchSettingsHandler{service: deps.WorkspaceSearchSettings}
				adminGroup.PUT("/search-settings", settings.update)
			}
			if deps.ChunkRevisions != nil {
				chunks := chunkRevisionHandler{service: deps.ChunkRevisions}
				adminGroup.POST("/knowledge-bases/:id/chunks/:chunk_id/revisions", chunks.create)
			}
			if deps.IndexGenerations != nil {
				generations := indexGenerationHandler{service: deps.IndexGenerations}
				adminGroup.POST("/knowledge-bases/:id/index-generations", generations.create)
				adminGroup.POST("/knowledge-bases/:id/index-generations/:generation_id/activate", generations.activate)
			}
		}

		// admin+ routes.
		if deps.Invitations != nil {
			invH := invitationHandler{invitations: deps.Invitations, publicURLs: deps.PublicURLs}
			adminGroup := wsGroup.Group("")
			adminGroup.Use(RequireWorkspaceRole(value.RoleAdmin))
			{
				adminGroup.GET("/invitations", invH.list)
				adminGroup.POST("/invitations", invH.create)
				adminGroup.DELETE("/invitations/:invitation_id", invH.revoke)
			}
		}

		// admin+ API Key management (Session-only).
		if deps.APIKeys != nil {
			keyH := apiKeyHandler{keys: deps.APIKeys}
			adminGroup := wsGroup.Group("")
			adminGroup.Use(RequireWorkspaceRole(value.RoleAdmin))
			{
				adminGroup.GET("/api-keys", keyH.list)
				adminGroup.POST("/api-keys", keyH.create)
				adminGroup.GET("/api-keys/:api_key_id", keyH.get)
				adminGroup.PATCH("/api-keys/:api_key_id", keyH.update)
				adminGroup.POST("/api-keys/:api_key_id/reveal", keyH.reveal)
				adminGroup.DELETE("/api-keys/:api_key_id", keyH.revoke)
			}
		}

		// admin+ 来源连接管理（飞书应用 CRUD，Session-only）。
		// 不注册到 progGroup，因此 API Key 主体不可达（凭证管理不对外开）。
		if deps.SourceConnections != nil {
			connectionH := sourceConnectionHandler{svc: deps.SourceConnections}
			adminGroup := wsGroup.Group("")
			adminGroup.Use(RequireWorkspaceRole(value.RoleAdmin))
			{
				adminGroup.POST("/source-connections", connectionH.create)
				adminGroup.GET("/source-connections", connectionH.list)
				adminGroup.GET("/source-connections/:connection_id", connectionH.get)
				adminGroup.PATCH("/source-connections/:connection_id", connectionH.update)
				adminGroup.DELETE("/source-connections/:connection_id", connectionH.delete)
			}
		}

		// owner-only routes.
		if deps.Memberships != nil {
			mbH := membershipHandler{memberships: deps.Memberships}
			ownerGroup := wsGroup.Group("")
			ownerGroup.Use(RequireWorkspaceRole(value.RoleOwner))
			{
				ownerGroup.PATCH("/members/:user_id", mbH.changeRole)
				ownerGroup.DELETE("/members/:user_id", mbH.remove)
			}
		}
	}

	spaHandler := newSPAHandler(deps.SPA)
	router.NoRoute(func(c *gin.Context) {
		if isRESTAPIPath(c.Request.URL.Path) {
			writeError(c, stdhttp.StatusNotFound, "not_found", "接口不存在")
			return
		}
		if isNonSPAPath(c.Request.URL.Path) {
			c.Status(stdhttp.StatusNotFound)
			return
		}
		if spaHandler != nil {
			spaHandler(c)
			return
		}
		c.Status(stdhttp.StatusNotFound)
	})

	return router
}

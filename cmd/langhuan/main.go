package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	hibikenasynq "github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	authadapter "github.com/dajee/langhuan/internal/adapters/auth"
	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	arkembedding "github.com/dajee/langhuan/internal/adapters/embedding/ark"
	dashscopeembedding "github.com/dajee/langhuan/internal/adapters/embedding/dashscope"
	ollamaembedding "github.com/dajee/langhuan/internal/adapters/embedding/ollama"
	openaembedding "github.com/dajee/langhuan/internal/adapters/embedding/openai"
	tencentcloudembedding "github.com/dajee/langhuan/internal/adapters/embedding/tencentcloud"
	parseradapter "github.com/dajee/langhuan/internal/adapters/parser"
	csvparser "github.com/dajee/langhuan/internal/adapters/parser/csv"
	docxparser "github.com/dajee/langhuan/internal/adapters/parser/docx"
	markdownparser "github.com/dajee/langhuan/internal/adapters/parser/markdown"
	textparser "github.com/dajee/langhuan/internal/adapters/parser/text"
	xlsxparser "github.com/dajee/langhuan/internal/adapters/parser/xlsx"
	parserprovideradapter "github.com/dajee/langhuan/internal/adapters/parserprovider"
	minerufactory "github.com/dajee/langhuan/internal/adapters/parserprovider/mineru"
	queueadapter "github.com/dajee/langhuan/internal/adapters/queue/asynq"
	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	rerankcompatible "github.com/dajee/langhuan/internal/adapters/rerank/compatible"
	siliconflowadapter "github.com/dajee/langhuan/internal/adapters/siliconflow"
	feishu "github.com/dajee/langhuan/internal/adapters/source/feishu"
	localstorage "github.com/dajee/langhuan/internal/adapters/storage/local"
	s3storage "github.com/dajee/langhuan/internal/adapters/storage/s3"
	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/pipeline"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/infrastructure/logger"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	"github.com/dajee/langhuan/internal/infrastructure/version"
	langhttp "github.com/dajee/langhuan/internal/interfaces/http"
	langmcp "github.com/dajee/langhuan/internal/interfaces/mcp"
	"github.com/dajee/langhuan/internal/interfaces/worker"
	authport "github.com/dajee/langhuan/internal/ports/auth"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
	queueport "github.com/dajee/langhuan/internal/ports/queue"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
	storageport "github.com/dajee/langhuan/internal/ports/storage"
	webspa "github.com/dajee/langhuan/web"
)

var (
	openDatabase   = db.Open
	newRedisClient = redis.NewClient
	pingRedis      = func(ctx context.Context, client *redis.Client) error {
		return client.Ping(ctx).Err()
	}
	newAsynqClient = func(opt hibikenasynq.RedisClientOpt) *hibikenasynq.Client {
		return hibikenasynq.NewClient(opt)
	}
)

type appRuntime struct {
	cfg          *config.Config
	httpServer   *http.Server
	workerServer *hibikenasynq.Server
	workerMux    *hibikenasynq.ServeMux
	asynqClient  *hibikenasynq.Client
	redisClient  *redis.Client
	gormDB       *gorm.DB
	jobQueue     queueport.JobQueue
	services     *runtimeServices
}

type runtimeServices struct {
	// auth (Task 8): repos + services driving the auth/user/invitation/membership
	// handlers and the SessionAuth middleware.
	userRepo       *db.UserRepository
	sessionRepo    *db.SessionRepository
	membershipRepo *db.MembershipRepository
	invitationRepo *db.InvitationRepository
	users          *service.UserService
	auth           *service.AuthService
	invitations    *service.InvitationService
	memberships    *service.MembershipService
	sessionCfg     config.SessionConfig
	publicURLs     *service.PublicURLBuilder

	// resource (workspace-scoped)
	workspaceRepo        *db.WorkspaceRepository
	knowledgeBaseRepo    *db.KnowledgeBaseRepository
	modelProviderRepo    *db.ModelProviderRepository
	modelRepo            *db.ModelRepository
	documentRepo         *db.DocumentRepository
	faqRepo              *db.FAQRepository
	retrievalRepo        *db.RetrievalRepository
	documentTaskStore    *db.DocumentTaskDBStore
	chunkRevisionStore   *db.ChunkRevisionDBStore
	indexGenerationStore *db.IndexGenerationDBStore

	workspaces              *service.WorkspaceService
	workspaceReadiness      *service.WorkspaceReadinessService
	workspaceSearchSettings *service.WorkspaceSearchSettingsService
	knowledgeBaseSummary    *service.KnowledgeBaseSummaryService
	knowledgeBases          *service.KnowledgeBaseService
	modelProviders          *service.ModelProviderService
	models                  *service.ModelService
	modelConnectionTests    *service.ModelConnectionTestService
	documents               *service.DocumentService
	documentAssets          *service.DocumentAssetService
	jobs                    *service.JobService
	documentIngest          *service.DocumentIngestService
	faqDocuments            *service.FAQDocumentService
	embeddingResolver       service.EmbeddingClientResolver
	fileTree                *service.FileTreeService
	chunkRevisions          *service.ChunkRevisionService
	documentChunks          *service.DocumentChunksService
	chunkRevisionIndexer    *service.ChunkRevisionIndexService
	indexGenerations        *service.IndexGenerationService
	indexGenerationBuilder  *service.IndexGenerationBuildService
	sourceSync              *service.SourceSyncService
	sourceSyncScheduler     *service.SourceSyncScheduler
	search                  *service.SearchService
	multiSearch             *service.MultiKnowledgeSearchService
	retrievalCleanup        *service.RetrievalCleanupService
	pipeline                *pipeline.DocumentPipeline
	rawStore                storageport.RawDocumentStore
	parserRegistry          *parseradapter.Registry
	assetStore              storageport.AssetStore
	maxFileSize             int64
	apiKeys                 *service.APIKeyService
	mcpInlineLimit          int64
}

type embeddingFactoryCatalog interface {
	embeddingport.FactoryRegistry
	Factories() []embeddingport.Factory
}

type rerankFactoryCatalog interface {
	rerankport.FactoryRegistry
	Factories() []rerankport.Factory
}

// mcpDocumentStatusReader 组合 DocumentService 与 JobService，满足
// ProgrammaticDocumentStatusReader 端口。
type mcpDocumentStatusReader struct {
	documents *service.DocumentService
	jobs      *service.JobService
}

func (r *mcpDocumentStatusReader) GetDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) (*dto.Document, error) {
	return r.documents.Get(ctx, access, documentID)
}
func (r *mcpDocumentStatusReader) GetJob(ctx context.Context, access value.ResourceAccess, jobID uuid.UUID) (*dto.Job, error) {
	return r.jobs.Get(ctx, access, jobID)
}

// httpKnowledgeBaseSyncService 把 *service.SourceSyncService.EnqueueSync（返回 *model.Job）
// 适配成 langhttp.KnowledgeBaseSyncService（返回 *dto.Job）。
type httpKnowledgeBaseSyncService struct {
	sync *service.SourceSyncService
}

func (s *httpKnowledgeBaseSyncService) EnqueueSync(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*dto.Job, error) {
	job, err := s.sync.EnqueueSync(ctx, workspaceID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	return dto.JobFromModel(job), nil
}

func main() {
	if err := run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	configFile, err := configPath(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}
	log := logger.New(cfg.Log.Level)
	log.Info("starting langhuan", slog.String("version", version.Version()))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := buildApp(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.shutdown(shutdownCtx); err != nil {
			log.Error("关闭运行时失败", slog.Any("error", err))
		}
	}()

	if !cfg.Server.RunHTTP && !cfg.Server.RunWorker {
		return nil
	}
	return app.start(ctx, log)
}

func configPath(args []string) (string, error) {
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("config", "config.yaml", "YAML 配置文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return "", err
	}
	return *path, nil
}

func buildApp(ctx context.Context, cfg *config.Config, log *slog.Logger) (*appRuntime, error) {
	app := &appRuntime{cfg: cfg}
	if !cfg.Server.RunHTTP && !cfg.Server.RunWorker {
		return app, nil
	}

	gormDB, err := openDatabase(cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	app.gormDB = gormDB
	if shouldRunMigrations(cfg) {
		if err := migrate.Run(ctx, cfg.Database.DSN); err != nil {
			return nil, err
		}
		// Task 8: 不再调用 EnsureDefaultWorkspace——多租户认证启用后由首位
		// platform admin 通过 /api/v1/auth/register + /api/v1/workspaces 显式建立
		// 自有租户；这里只保留 migrate.Run（为旧库回填 workspace.slug）。
		// EnsureDefaultWorkspace helper 仍保留，仅供旧库迁移兼容测试使用。
	}

	if needsQueueClient(cfg) {
		redisOpt := hibikenasynq.RedisClientOpt{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		}
		app.redisClient = newRedisClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := pingRedis(ctx, app.redisClient); err != nil {
			return nil, fmt.Errorf("连接 Redis 失败: %w", err)
		}
		app.asynqClient = newAsynqClient(redisOpt)
		app.jobQueue = queueadapter.NewQueue(app.asynqClient)
	}

	if needsWorkerServer(cfg) {
		redisOpt := hibikenasynq.RedisClientOpt{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		}
		app.workerMux = hibikenasynq.NewServeMux()
		app.workerServer = hibikenasynq.NewServer(redisOpt, hibikenasynq.Config{
			Queues: map[string]int{"default": 1},
		})
	}

	embeddingRegistry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		return nil, err
	}
	rerankRegistry, err := buildRuntimeRerankRegistry()
	if err != nil {
		return nil, err
	}
	parserProviderRegistry, err := buildRuntimeParserProviderRegistry(cfg)
	if err != nil {
		return nil, err
	}
	app.services, err = buildRuntimeServices(ctx, gormDB, cfg, app.jobQueue, app.redisClient, embeddingRegistry, rerankRegistry, parserProviderRegistry, log)
	if err != nil {
		return nil, err
	}

	if cfg.Server.RunHTTP {
		app.httpServer = &http.Server{Addr: cfg.Server.HTTPAddr, Handler: buildHTTPRouter(app.services)}
	}

	if app.workerMux != nil {
		worker.RegisterSmokeHandler(app.workerMux)
		worker.RegisterDocumentHandlers(app.workerMux, worker.DocumentHandlers{
			Store:          app.services.documentTaskStore,
			Queue:          app.jobQueue,
			Pipeline:       app.services.pipeline,
			ParserRegistry: app.services.parserRegistry,
			AssetStoreFactory: func(workspaceID, knowledgeBaseID, documentID, revisionID uuid.UUID) *pipeline.AssetResolver {
				resolver := pipeline.NewAssetResolver(
					app.services.assetStore, http.DefaultClient, cfg.Storage.Assets,
					workspaceID, knowledgeBaseID, documentID, revisionID,
				)
				// local 模式（存储层无 CDN public URL）时，把图片引用改写为
				// 指向鉴权资产代理 handler 的绝对地址，确保 markdown 附件可访问。
				resolver.WithPublicURLBuilder(func(asset model.Asset, stored *storageport.StoredObject) string {
					if stored.PublicURL != "" {
						return stored.PublicURL
					}
					ws, err := app.services.workspaces.Get(context.Background(), workspaceID)
					if err != nil {
						// 查不到 slug 时回退到 storage key（保持旧行为）
						return stored.Key
					}
					return fmt.Sprintf("%s/api/v1/workspaces/%s/documents/%s/assets/%s",
						strings.TrimSuffix(cfg.Server.BaseURL, "/"), ws.Slug,
						documentID.String(), asset.ID.String())
				})
				return resolver
			},
			Logger: log,
		})
		worker.RegisterChunkRevisionHandler(app.workerMux, worker.ChunkRevisionHandler{
			Indexer: app.services.chunkRevisionIndexer,
			Logger:  log,
		})
		worker.RegisterIndexGenerationBuildHandler(app.workerMux, worker.IndexGenerationBuildHandler{
			Builder: app.services.indexGenerationBuilder,
			Logger:  log,
		})
		worker.RegisterSourceSyncHandler(app.workerMux, worker.SourceSyncHandler{
			Runner:     app.services.sourceSync,
			Store:      app.services.documentTaskStore,
			Dispatcher: app.services.sourceSyncScheduler,
			Logger:     log,
		})
	}

	return app, nil
}

func buildRuntimeServices(ctx context.Context, gormDB *gorm.DB, cfg *config.Config, jobQueue queueport.JobQueue, redisClient *redis.Client, embeddingRegistry embeddingFactoryCatalog, rerankRegistry rerankFactoryCatalog, parserProviderRegistry *parserprovideradapter.Registry, log *slog.Logger) (*runtimeServices, error) {
	if embeddingRegistry == nil {
		return nil, fmt.Errorf("构造模型服务失败: Embedding Factory Registry 不能为空")
	}
	providerDescriptors, err := buildProviderDescriptorRegistry(embeddingRegistry, rerankRegistry, parserProviderRegistry)
	if err != nil {
		return nil, fmt.Errorf("构造 Provider descriptor registry 失败: %w", err)
	}
	providerResolver := service.NewProviderFactoryResolver(providerDescriptors)
	publicURLs, err := service.NewPublicURLBuilder(cfg.Server.BaseURL)
	if err != nil {
		return nil, err
	}
	encryptionKey, err := cfg.Credentials.DecodeEncryptionKey()
	if err != nil {
		return nil, err
	}
	defer clearSensitiveBytes(encryptionKey)
	credentialCipher, err := db.NewAESGCMCredentialCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("构造模型凭证加密器失败: %w", err)
	}

	// Resource repos constructed first so the workspace repo can be shared with
	// both the workspace service and the invitation service (GetPublic does
	// best-effort workspace name/slug enrichment).
	wsRepo := db.NewWorkspaceRepository(gormDB)
	kbRepo := db.NewKnowledgeBaseRepository(gormDB)
	modelProviderRepo := db.NewModelProviderRepository(gormDB)
	modelRepo := db.NewModelRepository(gormDB)
	documentRepo := db.NewDocumentRepository(gormDB)
	documentRevisionRepo := db.NewDocumentRevisionRepository(gormDB)
	indexGenerationRepo := db.NewIndexGenerationRepository(gormDB)
	chunkSetRepo := db.NewChunkSetRepository(gormDB)
	faqRepo := db.NewFAQRepository(gormDB)
	retrievalRepo := db.NewRetrievalRepository(gormDB)
	retrievalCleanupRepo := db.NewRetrievalCleanupRepository(gormDB)
	documentPublisher := db.NewDocumentPublishDBStore(gormDB)
	chunkRevisionStore := db.NewChunkRevisionStore(gormDB)
	indexGenerationStore := db.NewIndexGenerationStore(gormDB)
	jobRepo := db.NewJobRepository(gormDB)
	workspaceReadinessRepo := db.NewWorkspaceReadinessRepository(gormDB)
	workspaceSearchSettingsRepo := db.NewWorkspaceSearchSettingsRepository(gormDB)
	knowledgeBaseSummaryRepo := db.NewKnowledgeBaseSummaryRepository(gormDB)
	documentChunksRepo := db.NewDocumentChunksRepository(gormDB)

	// Auth repos (Task 8).
	userRepo := db.NewUserRepository(gormDB)
	sessionRepo := db.NewSessionRepository(gormDB)
	membershipRepo := db.NewMembershipRepository(gormDB)
	invitationRepo := db.NewInvitationRepository(gormDB)

	// Argon2 hasher (no Redis dependency).
	hasher := authadapter.NewArgon2Hasher(
		cfg.Auth.Password.Argon2MemoryKiB,
		cfg.Auth.Password.Argon2Iterations,
		cfg.Auth.Password.Argon2Parallelism,
	)

	// Rate limiter: declare the INTERFACE variable first, then assign the
	// concrete adapter only when redisClient is non-nil. This avoids the
	// typed-nil-interface trap (passing a nil *RedisRateLimiter as
	// authport.RateLimiter yields a non-nil interface wrapping a nil pointer,
	// which would panic on the first method call). The HTTP runtime always
	// has Redis (needsQueueClient is true when RunHTTP), so the limiter is
	// populated in production; the no-DB stub tests pass nil redisClient and
	// never exercise login.
	var limiter authport.RateLimiter
	if redisClient != nil {
		limiter = authadapter.NewRedisRateLimiter(redisClient)
	}

	users := service.NewUserService(userRepo, hasher)
	auth := service.NewAuthService(userRepo, sessionRepo, hasher, limiter, cfg.Auth)
	invitations := service.NewInvitationService(invitationRepo, wsRepo, userRepo, hasher, cfg.Auth)
	memberships := service.NewMembershipService(membershipRepo, userRepo)

	var rawStore storageport.RawDocumentStore = localstorage.NewRawDocumentStore(cfg.Storage.RawDocumentDir)

	// 构建 parser registry（本地格式 + 可选 MinerU PDF）
	var assetStore storageport.AssetStore
	if cfg.Storage.Driver == "s3" {
		s3Store, err := s3storage.NewStore(ctx, s3storage.Config{
			Endpoint:       cfg.Storage.S3.Endpoint,
			Region:         cfg.Storage.S3.Region,
			Bucket:         cfg.Storage.S3.Bucket,
			AccessKey:      cfg.Storage.S3.AccessKey,
			SecretKey:      cfg.Storage.S3.SecretKey,
			ForcePathStyle: cfg.Storage.S3.ForcePathStyle,
			PublicBaseURL:  cfg.Storage.S3.PublicBaseURL,
		})
		if err != nil {
			return nil, fmt.Errorf("构造 S3 存储失败: %w", err)
		}
		rawStore = s3Store.NewRawDocumentStore()
		assetStore = s3Store.NewAssetStore()
	} else {
		assetStore = localstorage.NewAssetStore(cfg.Storage.RawDocumentDir)
	}

	// MinerU 凭据选择器（ParserProviderSelector）
	mineruSelector := service.NewParserProviderSelector(modelProviderRepo, credentialCipher)

	runtimeParser, err := buildRuntimeParser(cfg, rawStore, mineruSelector)
	if err != nil {
		return nil, fmt.Errorf("构造 runtime parser registry 失败: %w", err)
	}
	embeddingResolver := service.NewEmbeddingClientResolver(modelRepo, credentialCipher, embeddingRegistry)
	rerankResolver := service.NewRerankClientResolver(modelRepo, credentialCipher, rerankRegistry)
	documentPipeline := pipeline.NewDocumentPipeline(pipeline.DocumentPipelineDeps{
		Documents:         documentRepo,
		Revisions:         documentRevisionRepo,
		Generations:       indexGenerationRepo,
		ChunkSets:         chunkSetRepo,
		FAQRevisions:      faqRepo,
		IndexSources:      chunkSetRepo,
		EmbeddingResolver: embeddingResolver,
		RetrievalIndex:    retrievalRepo,
		Publisher:         documentPublisher,
		Parser:            runtimeParser,
		RawStore:          rawStore,
		Assets:            db.NewDocumentAssetRepository(gormDB),
		MaxFileSizeBytes:  cfg.Ingest.MaxFileSizeBytes,
	})
	chunkRevisions := service.NewChunkRevisionService(chunkRevisionStore, jobQueue)
	chunkRevisionIndexer := service.NewChunkRevisionIndexService(
		chunkRevisionStore, embeddingResolver, retrievalRepo,
	)
	indexGenerations := service.NewIndexGenerationService(service.IndexGenerationServiceDeps{
		Store: indexGenerationStore, Models: kbRepo, Queue: jobQueue,
	})
	indexGenerationBuilder := service.NewIndexGenerationBuildService(service.IndexGenerationBuildDeps{
		Store: indexGenerationStore, Chunker: documentPipeline, Sources: chunkSetRepo,
		Resolver: embeddingResolver, Index: retrievalRepo,
	})
	workspaceSearchSettings := service.NewWorkspaceSearchSettingsService(workspaceSearchSettingsRepo, kbRepo)
	search := service.NewSearchService(service.SearchServiceDeps{
		Repository: retrievalRepo, Resolver: embeddingResolver, RerankResolver: rerankResolver, SearchProfile: workspaceSearchSettings, Logger: log,
	})
	apiKeyNameStore := db.NewAPIKeyNameStoreDB(gormDB)
	multiSearch := service.NewMultiKnowledgeSearchService(retrievalRepo, embeddingResolver, rerankResolver, workspaceSearchSettings, apiKeyNameStore, cfg.Search, log)
	retrievalCleanup := service.NewRetrievalCleanupService(retrievalCleanupRepo, service.RetrievalCleanupOptions{
		FailedStagingRetention:     cfg.Retrieval.FailedStagingRetention,
		RetiredGenerationRetention: cfg.Retrieval.RetiredGenerationRetention,
		BatchSize:                  cfg.Retrieval.CleanupBatchSize,
	})
	apiKeyCipher, err := authadapter.NewAPIKeyCipher(encryptionKey, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 API Key 加密器失败: %w", err)
	}
	apiKeyRepo := db.NewWorkspaceAPIKeyRepository(gormDB)
	apiKeys, err := service.NewAPIKeyService(service.APIKeyServiceDeps{
		Store:  apiKeyRepo,
		Cipher: apiKeyCipher,
		Names:  apiKeyNameStore,
		URLs:   publicURLs,
		Config: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 API Key 服务失败: %w", err)
	}

	// 来源同步（飞书全量同步）：cipher + selector + connector + store + service。
	sourceConnectionCipher, err := db.NewSourceConnectionCredentialCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("构造来源连接凭证加密器失败: %w", err)
	}
	sourceConnectionSelector := service.NewSourceConnectionSelector(
		db.NewSourceConnectionRepository(gormDB), sourceConnectionCipher,
	)
	feishuConnector := feishu.NewConnector(feishu.WithCredentialDecryptor(sourceConnectionCipher))
	sourceSyncStore := db.NewSourceSyncDBStore(gormDB)
	sourceSync := service.NewSourceSyncService(service.SourceSyncServiceDeps{
		KnowledgeBaseRepository: kbRepo,
		Selector:                sourceConnectionSelector,
		Connector:               feishuConnector,
		RawStore:                rawStore,
		Store:                   sourceSyncStore,
		Queue:                   jobQueue,
		Logger:                  log,
		RootResolver:            feishu.ParseURL,
	})

	// Meta Scheduler：周期扫描到期飞书 KB，按来源连接限流入队。
	sourceSyncScheduler := service.NewSourceSyncScheduler(service.SourceSyncSchedulerDeps{
		KBRepo:                     kbRepo,
		Store:                      sourceSyncStore,
		SyncService:                sourceSync,
		MaxConcurrentPerConnection: cfg.SourceSync.MaxConcurrentPerConnection,
		Interval:                   time.Duration(cfg.SourceSync.SchedulerIntervalSeconds) * time.Second,
		Logger:                     log,
	})

	return &runtimeServices{
		userRepo:       userRepo,
		sessionRepo:    sessionRepo,
		membershipRepo: membershipRepo,
		invitationRepo: invitationRepo,
		users:          users,
		auth:           auth,
		invitations:    invitations,
		memberships:    memberships,
		sessionCfg:     cfg.Auth.Session,
		publicURLs:     publicURLs,

		workspaceRepo:           wsRepo,
		knowledgeBaseRepo:       kbRepo,
		modelProviderRepo:       modelProviderRepo,
		modelRepo:               modelRepo,
		documentRepo:            documentRepo,
		documentTaskStore:       db.NewDocumentTaskStore(gormDB),
		chunkRevisionStore:      chunkRevisionStore,
		indexGenerationStore:    indexGenerationStore,
		faqRepo:                 faqRepo,
		retrievalRepo:           retrievalRepo,
		workspaces:              service.NewWorkspaceService(wsRepo),
		workspaceReadiness:      service.NewWorkspaceReadinessService(workspaceReadinessRepo),
		workspaceSearchSettings: workspaceSearchSettings,
		knowledgeBaseSummary:    service.NewKnowledgeBaseSummaryService(knowledgeBaseSummaryRepo),
		knowledgeBases:          service.NewKnowledgeBaseService(kbRepo, kbRepo).WithSyncEnqueuer(sourceSync, log),
		modelProviders:          service.NewModelProviderService(modelProviderRepo, credentialCipher, providerResolver),
		models:                  service.NewModelService(modelProviderRepo, modelRepo, embeddingRegistry, rerankRegistry, providerDescriptors),
		modelConnectionTests:    service.NewModelConnectionTestService(modelRepo, credentialCipher, embeddingRegistry, rerankRegistry),
		documents:               service.NewDocumentService(documentRepo, kbRepo),
		documentAssets:          service.NewDocumentAssetService(db.NewDocumentAssetRepository(gormDB), documentRepo),
		jobs:                    service.NewJobService(jobRepo),
		documentIngest: service.NewDocumentIngestService(service.DocumentIngestServiceDeps{
			Store:            db.NewDocumentIngestDBStore(gormDB),
			RawStore:         rawStore,
			Queue:            jobQueue,
			AllowedFileTypes: cfg.Ingest.AllowedFileTypes,
		}),
		faqDocuments: service.NewFAQDocumentService(service.FAQDocumentServiceDeps{
			Store: faqRepo,
			Queue: jobQueue,
		}),
		embeddingResolver:      embeddingResolver,
		fileTree:               service.NewFileTreeService(db.NewFileTreeRepository(gormDB)),
		chunkRevisions:         chunkRevisions,
		documentChunks:         service.NewDocumentChunksService(documentChunksRepo),
		chunkRevisionIndexer:   chunkRevisionIndexer,
		indexGenerations:       indexGenerations,
		indexGenerationBuilder: indexGenerationBuilder,
		sourceSync:             sourceSync,
		sourceSyncScheduler:    sourceSyncScheduler,
		search:                 search,
		multiSearch:            multiSearch,
		retrievalCleanup:       retrievalCleanup,
		pipeline:               documentPipeline,
		rawStore:               rawStore,
		parserRegistry:         runtimeParser,
		assetStore:             assetStore,
		maxFileSize:            cfg.Ingest.MaxFileSizeBytes,
		apiKeys:                apiKeys,
		mcpInlineLimit:         cfg.MCP.InlineIngestMaxFileSizeBytes,
	}, nil
}

func buildRuntimeEmbeddingRegistry() (embeddingFactoryCatalog, error) {
	return embeddingadapter.NewRegistry(
		openaembedding.NewFactory(),
		arkembedding.NewFactory(),
		ollamaembedding.NewFactory(),
		dashscopeembedding.NewFactory(),
		tencentcloudembedding.NewFactory(),
		siliconflowadapter.NewEmbeddingFactory(),
	)
}

// buildRuntimeRerankRegistry 构建 Rerank Factory 注册表。
// 当前注册 rerank_compatible 一个 Provider；后续原生 adapter 出现真实需求时在此追加。
func buildRuntimeRerankRegistry() (rerankFactoryCatalog, error) {
	return rerankadapter.NewRegistry(
		rerankcompatible.NewFactory(),
		siliconflowadapter.NewRerankFactory(),
	)
}

// buildRuntimeParserProviderRegistry 构建解析器 Provider Factory 注册表。
// 仅当 mineru.enabled=true 时注册 mineru factory：未开启配置时，
// 用户不能在 Web Console 创建 MinerU Provider（避免配置了却无法使用）。
func buildRuntimeParserProviderRegistry(cfg *config.Config) (*parserprovideradapter.Registry, error) {
	var factories []parserproviderport.Factory
	if cfg.MinerU.Enabled {
		factories = append(factories, minerufactory.NewFactory())
	}
	return parserprovideradapter.NewRegistry(factories...)
}

// buildProviderDescriptorRegistry 从已装配的能力 Factory 构建显式 Provider 描述符。
func buildProviderDescriptorRegistry(embeddingRegistry embeddingFactoryCatalog, rerankRegistry rerankFactoryCatalog, parserRegistry *parserprovideradapter.Registry) (*service.ProviderDescriptorRegistry, error) {
	byKey := make(map[string]service.ProviderDescriptor)
	add := func(descriptor service.ProviderDescriptor) {
		key := strings.ToLower(strings.TrimSpace(descriptor.Key))
		if existing, ok := byKey[key]; ok {
			existing.Capabilities = append(existing.Capabilities, descriptor.Capabilities...)
			if existing.ModelCatalog == nil {
				existing.ModelCatalog = descriptor.ModelCatalog
			}
			byKey[key] = existing
			return
		}
		byKey[key] = descriptor
	}
	for _, factory := range embeddingRegistry.Factories() {
		add(service.EmbeddingProviderDescriptor(factory))
	}
	if rerankRegistry != nil {
		for _, factory := range rerankRegistry.Factories() {
			add(service.RerankProviderDescriptor(factory))
		}
	}
	if parserRegistry != nil {
		for _, factory := range parserRegistry.Factories() {
			add(service.ParserProviderDescriptor(factory))
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	descriptors := make([]service.ProviderDescriptor, 0, len(keys))
	for _, key := range keys {
		descriptors = append(descriptors, byKey[key])
	}
	return service.NewProviderDescriptorRegistry(descriptors...)
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func buildRuntimeParser(cfg *config.Config, rawStore storageport.RawDocumentStore, mineruSelector *service.ParserProviderSelector) (*parseradapter.Registry, error) {
	registrations := []parseradapter.Registration{
		{FileType: "markdown", Parser: markdownparser.New()},
		{FileType: "txt", Parser: textparser.New()},
		{FileType: "csv", Parser: csvparser.New()},
		{FileType: "xlsx", Parser: xlsxparser.New()},
		{FileType: "docx", Parser: docxparser.New()},
	}

	// MinerU PDF parser（启用时注册）
	if cfg.MinerU.Enabled && mineruSelector != nil {
		mineruParser := minerufactory.NewLazyParser(&mineruSelectorAdapter{selector: mineruSelector}, rawStore, minerufactory.LazyParserConfig{
			ModelVersion:          cfg.MinerU.ModelVersion,
			PollInterval:          time.Duration(cfg.MinerU.PollIntervalSeconds) * time.Second,
			MaxPollAttempts:       cfg.MinerU.MaxPollAttempts,
			UploadTimeout:         time.Duration(cfg.MinerU.UploadTimeoutSeconds) * time.Second,
			ResultDownloadTimeout: time.Duration(cfg.MinerU.ResultDownloadTimeoutSeconds) * time.Second,
			MaxZipImageBytes:      cfg.MinerU.MaxZipImageBytes,
		})
		registrations = append(registrations, parseradapter.Registration{
			FileType: "pdf",
			Parser:   mineruParser,
		})
	}

	return parseradapter.NewRegistry(registrations...)
}

// mineruSelectorAdapter 把 service.ParserProviderSelector 适配为 minerufactory.CredentialSelector。
type mineruSelectorAdapter struct {
	selector *service.ParserProviderSelector
}

func (a *mineruSelectorAdapter) SelectMinerU(ctx context.Context, workspaceID uuid.UUID) (minerufactory.SelectedCredential, error) {
	selected, err := a.selector.SelectMinerU(ctx, workspaceID)
	if err != nil {
		return minerufactory.SelectedCredential{}, err
	}
	return minerufactory.SelectedCredential{
		ProviderID:      selected.Provider.ID,
		Config:          selected.Provider.Config,
		CredentialsJSON: selected.CredentialsJSON,
	}, nil
}

func buildHTTPRouter(services *runtimeServices) http.Handler {
	mcpServer := langmcp.NewServer(langmcp.Dependencies{
		KnowledgeBases: langmcp.NewMCPKnowledgeBaseService(services.knowledgeBases),
		DocumentIngest: langmcp.NewMCPDocumentIngestService(services.documentIngest),
		DocumentStatus: service.NewProgrammaticDocumentStatusService(&mcpDocumentStatusReader{
			documents: services.documents, jobs: services.jobs,
		}),
		DocumentDelete: langmcp.NewMCPDocumentDeleteService(services.documents),
		ChunkGet:       langmcp.NewMCPChunkGetService(services.chunkRevisions),
		MultiSearch:    services.multiSearch,
		InlineLimit:    services.mcpInlineLimit,
	})
	return langhttp.NewRouter(langhttp.Dependencies{
		// auth (Task 8)
		Auth:          services.auth,
		Users:         services.users,
		Invitations:   services.invitations,
		Memberships:   services.memberships,
		SessionConfig: services.sessionCfg,
		PublicURLs:    services.publicURLs,
		APIKeys:       services.apiKeys,
		APIKeyAuth:    services.apiKeys,

		// resource (workspace-scoped)
		Workspaces:              services.workspaces,
		WorkspaceReadiness:      services.workspaceReadiness,
		WorkspaceSearchSettings: services.workspaceSearchSettings,
		KnowledgeBaseSummary:    services.knowledgeBaseSummary,
		KnowledgeBases:          services.knowledgeBases,
		KnowledgeBaseSync:       &httpKnowledgeBaseSyncService{sync: services.sourceSync},
		ModelProviders:          services.modelProviders,
		Models:                  services.models,
		ModelConnectionTests:    services.modelConnectionTests,
		DocumentIngest:          services.documentIngest,
		Documents:               services.documents,
		DocumentAssets:          services.documentAssets,
		AssetGetter:             services.documentAssets,
		AssetContentStore:       services.assetStore,
		FAQDocuments:            services.faqDocuments,
		FileTree:                services.fileTree,
		ChunkRevisions:          services.chunkRevisions,
		DocumentChunks:          services.documentChunks,
		IndexGenerations:        services.indexGenerations,
		Search:                  services.search,
		MultiSearch:             services.multiSearch,
		Jobs:                    services.jobs,
		MCPHandler:              mcpServer.Handler(),
		SPA:                     webspa.SPA,
		MaxFileSizeBytes:        services.maxFileSize,
	})
}

func needsQueueClient(cfg *config.Config) bool {
	return cfg.Server.RunHTTP || cfg.Server.RunWorker
}

func needsWorkerServer(cfg *config.Config) bool {
	return cfg.Server.RunWorker
}

func shouldRunMigrations(cfg *config.Config) bool {
	return cfg.Database.AutoMigrate
}

// startupBanner 在服务完全就绪、可以开始服务时原样输出到控制台。
const startupBanner = `▖       ▌       
▌ ▀▌▛▌▛▌▛▌▌▌▀▌▛▌
▙▖█▌▌▌▙▌▌▌▙▌█▌▌▌
      ▄▌        
`

// printStartupBanner 输出启动 banner 与就绪提示（控制台使用英文）。
func printStartupBanner(w io.Writer) {
	fmt.Fprint(w, startupBanner)
	fmt.Fprintln(w, "Langhuan is ready to serve requests.")
}

func (a *appRuntime) start(ctx context.Context, log *slog.Logger) error {
	errCh := make(chan error, 2)

	// 收集各服务"真正启动完成"的信号，全部就绪后才输出 banner。
	wantReady := 0
	if a.workerServer != nil && a.workerMux != nil {
		wantReady++
	}
	if a.httpServer != nil {
		wantReady++
	}
	readyCh := make(chan struct{}, wantReady)

	if a.workerServer != nil && a.workerMux != nil {
		log.Info("启动 asynq worker")
		// Start 同步返回即代表 worker 已启动（asynq 输出 Starting processing 并
		// 拉起全部子组件）；退出时由 app.shutdown 调用 workerServer.Shutdown 优雅关闭。
		if err := a.workerServer.Start(a.workerMux); err != nil {
			return fmt.Errorf("启动 asynq worker 失败: %w", err)
		}
		// 来源同步 Meta Scheduler：随 worker 生命周期运行，ctx 取消即停（随 shutdown 取消）。
		if a.services != nil && a.services.sourceSyncScheduler != nil {
			go func() { _ = a.services.sourceSyncScheduler.Run(ctx) }()
		}
		readyCh <- struct{}{}
	}
	if a.httpServer != nil {
		log.Info("启动 HTTP server", slog.String("addr", a.httpServer.Addr))
		// 同步绑定端口，成功即代表 HTTP 已可接收请求。
		listener, err := net.Listen("tcp", a.httpServer.Addr)
		if err != nil {
			return fmt.Errorf("监听 HTTP 端口失败: %w", err)
		}
		go func() {
			if err := a.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("HTTP server 退出: %w", err)
			}
		}()
		readyCh <- struct{}{}
	}

	for range wantReady {
		<-readyCh
	}
	printStartupBanner(os.Stdout)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *appRuntime) shutdown(ctx context.Context) error {
	var firstErr error
	capture := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.httpServer != nil {
		capture(a.httpServer.Shutdown(ctx))
	}
	if a.workerServer != nil {
		a.workerServer.Shutdown()
	}
	if a.asynqClient != nil {
		capture(a.asynqClient.Close())
	}
	if a.redisClient != nil {
		capture(a.redisClient.Close())
	}
	if a.gormDB != nil {
		sqlDB, err := a.gormDB.DB()
		if err != nil {
			capture(err)
		} else {
			capture(sqlDB.Close())
		}
	}
	return firstErr
}

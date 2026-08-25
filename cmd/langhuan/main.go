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
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	hibikenasynq "github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	authadapter "github.com/dajee/langhuan/internal/adapters/auth"
	oidcadapter "github.com/dajee/langhuan/internal/adapters/auth/oidc"
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
	dbqueue "github.com/dajee/langhuan/internal/adapters/queue/dbqueue"
	memoryqueue "github.com/dajee/langhuan/internal/adapters/queue/memory"
	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	rerankcompatible "github.com/dajee/langhuan/internal/adapters/rerank/compatible"
	siliconflowadapter "github.com/dajee/langhuan/internal/adapters/siliconflow"
	feishu "github.com/dajee/langhuan/internal/adapters/source/feishu"
	localstorage "github.com/dajee/langhuan/internal/adapters/storage/local"
	s3storage "github.com/dajee/langhuan/internal/adapters/storage/s3"
	gsetokenizer "github.com/dajee/langhuan/internal/adapters/tokenizer/gse"
	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/pipeline"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/datadir"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	metricspkg "github.com/dajee/langhuan/internal/infrastructure/metrics"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	otelinfra "github.com/dajee/langhuan/internal/infrastructure/otel"
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
	cfg            *config.Config
	httpServer     *http.Server
	workerServer   *hibikenasynq.Server
	workerMux      *hibikenasynq.ServeMux
	asynqClient    *hibikenasynq.Client
	queueInspector *queueadapter.QueueInspector
	otelProviders  *otelinfra.Providers
	redisClient    *redis.Client
	gormDB         *gorm.DB
	dialect        db.Dialect
	jobQueue       queueport.JobQueue
	services       *runtimeServices
	// standalone（SQLite / 无 Redis）模式专用组件。
	memoryQueue      *memoryqueue.Queue            // 内存队列（保留备用/测试）
	sqliteQueue      *dbqueue.Queue                // SQLite 持久化队列（无 Redis 时替代 asynq）
	memoryStateStore *oidcadapter.MemoryStateStore // 内存 OIDC state（无 Redis 时需 Close 停止清理 goroutine）
	gseTokenizer     db.SearchTokenizer            // 双方言注入：SQLite FTS5 索引/查询 + PG FTS 查询过滤
	inspectorPort    service.QueueInspectorPort    // 队列可见性端口（asynq inspectorPortAdapter / memory.Inspector）
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
	// OIDC（条件装配：cfg.Auth.OIDC.Enabled=true 时非 nil）
	oidc             *service.OIDCLoginService
	oidcAcceptor     *service.InvitationService
	oidcEnabled      bool
	passwordEnabled  bool
	memoryStateStore *oidcadapter.MemoryStateStore // 无 Redis 模式的 OIDC state store（需 Close）

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
	documentRetry           *service.DocumentRetryService
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
	sourceCleanup           *service.SourceCleanupService
	sourceCleanupScheduler  *service.SourceCleanupScheduler
	sourceConnections       *service.SourceConnectionService
	search                  *service.SearchService
	multiSearch             *service.MultiKnowledgeSearchService
	searchReplay            *service.SearchReplayService
	retrievalCleanup        *service.RetrievalCleanupService
	searchRunCleanup        *service.SearchRunCleanupService
	pipeline                *pipeline.DocumentPipeline
	rawStore                storageport.RawDocumentStore
	parserRegistry          *parseradapter.Registry
	assetStore              storageport.AssetStore
	maxFileSize             int64
	apiKeys                 *service.APIKeyService
	mcpInlineLimit          int64
	mcpHostProtection       bool
	readiness               langhttp.ReadinessChecker
	metricsPath             string
	httpMetrics             *metricspkg.Metrics
	queueAdmin              *service.QueueAdminService
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

func (s *httpKnowledgeBaseSyncService) EnqueueSync(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID, options service.SyncOptions) (*dto.Job, error) {
	job, err := s.sync.EnqueueSync(ctx, workspaceID, knowledgeBaseID, options)
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
	sel, err := selectConfig(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(sel.Path)
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	// 把脱敏 logger 设为全局默认：worker/service 里 slog.Default() 兜底路径
	// （Logger 未注入时）也继承脱敏，避免隐性旁路。
	slog.SetDefault(log)
	if sel.Explicit {
		log.Info("starting langhuan", slog.String("version", version.Version()), slog.String("config", sel.Path))
	} else {
		log.Info("starting langhuan", slog.String("version", version.Version()), slog.String("config", sel.Path))
	}

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

// selectConfig 解析 -config flag 并按 spec §2.1 四态探测链确定配置来源。
func selectConfig(args []string) (ConfigSelection, error) {
	explicitPath, explicitSet, err := parseConfigFlag(args)
	if err != nil {
		return ConfigSelection{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ConfigSelection{}, fmt.Errorf("获取当前目录失败: %w", err)
	}
	cwdConfig := filepath.Join(cwd, "config.yaml")
	dataDir, err := datadir.Resolve(os.UserHomeDir)
	if err != nil {
		return ConfigSelection{}, err
	}
	dataDirConfig := filepath.Join(dataDir.Path(), "config.yaml")
	return resolveConfigSelection(explicitPath, explicitSet, cwdConfig, dataDirConfig, dataDir.Path(),
		func(dataDirPath string) (string, error) {
			d := datadir.New(dataDirPath)
			if err := d.Ensure(); err != nil {
				return "", err
			}
			return config.MaterializeStandalone(dataDirPath, d.EnsureCredentialKey)
		})
}

func parseConfigFlag(args []string) (path string, explicit bool, err error) {
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	p := fs.String("config", "", "YAML 配置文件路径（未传时按四态探测链解析）")
	if err := fs.Parse(args[1:]); err != nil {
		return "", false, err
	}
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			set = true
		}
	})
	return *p, set, nil
}

func buildApp(ctx context.Context, cfg *config.Config, log *slog.Logger) (*appRuntime, error) {
	app := &appRuntime{cfg: cfg}
	if !cfg.Server.RunHTTP && !cfg.Server.RunWorker {
		return app, nil
	}

	// spec §11：迁移先于业务连接，避免 SQLite migration connection 与业务连接竞争。
	if shouldRunMigrations(cfg) {
		if err := migrate.Run(ctx, cfg.Database); err != nil {
			return nil, err
		}
	}
	gormDB, dialect, err := openDatabase(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	app.gormDB = gormDB
	app.dialect = dialect

	if needsQueueClient(cfg) {
		if cfg.Redis.Enabled {
			// PG + Redis 路径：redis client + asynq client + jobQueue + queueInspector。
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
			app.jobQueue = queueadapter.NewQueueWithDefaults(app.asynqClient, queueDefaults(cfg.Queue))
			insp, err := queueadapter.NewQueueInspectorFromRedis(redisOpt)
			if err != nil {
				return nil, fmt.Errorf("创建 asynq Inspector 失败: %w", err)
			}
			app.queueInspector = insp
			app.inspectorPort = inspectorPortAdapter{inspector: insp}
		} else {
			// 无 Redis 路径：SQLite 持久化队列（重启不丢任务）。
			// workerMux 必须先于 Queue 构造（Queue 需 mux 作为 TaskRunner）。
			mux := hibikenasynq.NewServeMux()
			mux.Use(otelTaskMiddleware())
			app.workerMux = mux
			sqlDB, err := gormDB.DB()
			if err != nil {
				return nil, fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
			}
			// 队列方言跟随数据库方言：SQLite 库→SQLite 队列表，PG 库→PG 队列表。
			var queueDialect dbqueue.Dialect
			switch dialect {
			case db.DialectSQLite:
				queueDialect = dbqueue.DialectSQLite
			case db.DialectPostgres:
				queueDialect = dbqueue.DialectPostgres
			default:
				return nil, fmt.Errorf("不支持的队列方言: %s", dialect)
			}
			sqlQ, err := dbqueue.New(sqlDB, queueDialect, mux, dbqueue.Config{
				Concurrency: cfg.Queue.Concurrency,
				MaxRetry:    cfg.Queue.MaxRetry(),
				MinBackoff:  cfg.Queue.MinBackoff(),
				MaxBackoff:  cfg.Queue.MaxBackoff(),
			})
			if err != nil {
				return nil, fmt.Errorf("创建 SQLite 队列失败: %w", err)
			}
			app.sqliteQueue = sqlQ
			app.jobQueue = sqlQ
			app.inspectorPort = dbqueue.NewInspector(sqlQ)
		}
	}

	if needsWorkerServer(cfg) {
		if cfg.Redis.Enabled {
			// asynq 路径：workerMux 在此构造，注册 handler 后由 asynq.Server 消费。
			redisOpt := hibikenasynq.RedisClientOpt{
				Addr:     cfg.Redis.Addr,
				Password: cfg.Redis.Password,
				DB:       cfg.Redis.DB,
			}
			app.workerMux = hibikenasynq.NewServeMux()
			// 为每个 asynq 任务开 OTel 根 span（task.<type>），使 handler 内的
			// document.stage / source.stage span event 有可归属的父 span。
			app.workerMux.Use(otelTaskMiddleware())
			app.workerServer = hibikenasynq.NewServer(redisOpt, asynqServerConfig(cfg.Queue, log))
		}
		// 内存路径：workerMux 已在 needsQueueClient 块构造，这里不建 asynq.Server；
		// worker handler 注册（后续 RegisterXxxHandler）两种模式共用同一 workerMux。
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
	// SQLite 模式需在应用层分词（PG 用 zhparser 在 SQL 层分词）。词典加载较重，
	// 整个进程构造一次，注入给 RetrievalRepository 的写入与查询路径。
	// gse 分词器两种方言都装配：SQLite 用于 FTS5 索引与查询构造，PG 用于
	// FTS 查询侧的停用词/疑问词过滤（检索通道修复，见 fts_query_filter.go）。
	seg, gerr := gsetokenizer.New()
	if gerr != nil {
		return nil, fmt.Errorf("加载 gse 分词器失败: %w", gerr)
	}
	app.gseTokenizer = seg
	app.services, err = buildRuntimeServices(ctx, gormDB, cfg, app.jobQueue, app.redisClient, embeddingRegistry, rerankRegistry, parserProviderRegistry, app.gseTokenizer, log)
	if err != nil {
		return nil, err
	}
	// 内存 OIDC state store 需在 shutdown 时 Close（停止后台清理 goroutine）。
	app.memoryStateStore = app.services.memoryStateStore

	// 可观测性：初始化 OTel providers（TracerProvider + MeterProvider + Prometheus/OTLP exporter）。
	otelProviders, err := otelinfra.Setup(ctx, cfg.Observability, log)
	if err != nil {
		return nil, fmt.Errorf("初始化 OTel 失败: %w", err)
	}
	app.otelProviders = otelProviders
	// 指标经 OTel Meter 产出，由 Prometheus exporter 暴露 /metrics，可选 OTLP 推送。
	app.services.httpMetrics = metricspkg.New(otelProviders.MeterProvider)
	// redis pinger 仅在 Redis 启用时构造；内存模式下传 nil redisPinger，
	// readiness 自动跳过 Redis 探活（避免对 nil redisClient 调用 Ping）。
	var redisPingerVal redisPinger
	if app.redisClient != nil {
		redisPingerVal = redisPingerImpl{ping: func(ctx context.Context) error {
			return app.redisClient.Ping(ctx).Err()
		}}
	}
	app.services.readiness = newReadinessChecker(
		gormPinger{ping: func(ctx context.Context) error {
			sqlDB, err := gormDB.DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		}},
		redisPingerVal,
		app.queueInspector,
		app.dialect,
		cfg.Observability,
	)
	if cfg.Observability.Metrics.Enabled {
		app.services.metricsPath = cfg.Observability.Metrics.Path
	}
	if app.inspectorPort != nil {
		app.services.queueAdmin = service.NewQueueAdminService(service.QueueAdminDeps{
			Inspector: app.inspectorPort,
		})
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
			Recorder:       app.services.httpMetrics,
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
		worker.RegisterSourceCleanupHandler(app.workerMux, worker.SourceCleanupHandler{
			Runner: app.services.sourceCleanup,
			Store:  app.services.documentTaskStore,
			Logger: log,
		})
	}

	return app, nil
}

func buildRuntimeServices(ctx context.Context, gormDB *gorm.DB, cfg *config.Config, jobQueue queueport.JobQueue, redisClient *redis.Client, embeddingRegistry embeddingFactoryCatalog, rerankRegistry rerankFactoryCatalog, parserProviderRegistry *parserprovideradapter.Registry, searchTokenizer db.SearchTokenizer, log *slog.Logger) (*runtimeServices, error) {
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
	retrievalRepo := db.NewRetrievalRepository(gormDB, searchTokenizer)
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

	// Rate limiter: redisClient 非空（PG+Redis 路径）用 Redis 限流器；
	// 否则（standalone 无 Redis）用进程内内存限流器，语义与 Redis 实现对齐。
	// 显式按 redisClient 分流，避免 typed-nil-interface 陷阱。
	var limiter authport.RateLimiter
	if redisClient != nil {
		limiter = authadapter.NewRedisRateLimiter(redisClient)
	} else {
		limiter = authadapter.NewMemoryRateLimiter()
	}

	users := service.NewUserService(userRepo, hasher, cfg.Auth.Password.Enabled)
	auth := service.NewAuthService(userRepo, sessionRepo, hasher, limiter, cfg.Auth)
	invitations := service.NewInvitationService(invitationRepo, wsRepo, userRepo, hasher, cfg.Auth)
	memberships := service.NewMembershipService(membershipRepo, userRepo)

	// OIDC 条件装配：cfg.Auth.OIDC.Enabled=true 时构造 provider/state store/service。
	var oidcLogin *service.OIDCLoginService
	var oidcAcceptor *service.InvitationService
	var memoryStateStore *oidcadapter.MemoryStateStore
	if cfg.Auth.OIDC.Enabled {
		oidcProvider, err := oidcadapter.NewProvider(oidcadapter.ProviderConfig{
			Enabled:            cfg.Auth.OIDC.Enabled,
			Issuer:             cfg.Auth.OIDC.Issuer,
			ClientID:           cfg.Auth.OIDC.ClientID,
			ClientSecret:       cfg.Auth.OIDC.ClientSecret,
			RedirectURL:        cfg.Auth.OIDC.RedirectURL,
			Scopes:             cfg.Auth.OIDC.Scopes,
			HTTPTimeoutSeconds: cfg.Auth.OIDC.HTTPTimeoutSeconds,
		})
		if err != nil {
			return nil, fmt.Errorf("构造 OIDC provider 失败: %w", err)
		}
		if oidcProvider != nil {
			// state store 按 Redis 是否启用分流：内存实现需在 shutdown 时 Close。
			var stateStore authport.OIDCStateStore
			if redisClient != nil {
				stateStore = oidcadapter.NewRedisStateStore(redisClient, cfg.Auth.OIDC.StateTTLSeconds)
			} else {
				mem := oidcadapter.NewMemoryStateStore(time.Duration(cfg.Auth.OIDC.StateTTLSeconds) * time.Second)
				memoryStateStore = mem
				stateStore = mem
			}
			identityRepo := db.NewExternalIdentityRepository(gormDB)
			authTxRunner := db.NewOIDCAuthTxRunner(gormDB)
			oidcLogin = service.NewOIDCLoginService(oidcProvider, stateStore, authTxRunner, identityRepo, cfg.Auth.Session, cfg.Auth.OIDC, cfg.Auth.OIDC.Enabled, log)
			// 邀请接受复用同一 runner。
			invitations.WithOIDCAuthTx(authTxRunner)
			oidcAcceptor = invitations
		}
	}

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
		Documents:            documentRepo,
		Revisions:            documentRevisionRepo,
		Generations:          indexGenerationRepo,
		ChunkSets:            chunkSetRepo,
		FAQRevisions:         faqRepo,
		IndexSources:         chunkSetRepo,
		EmbeddingResolver:    embeddingResolver,
		RetrievalIndex:       retrievalRepo,
		Publisher:            documentPublisher,
		Parser:               runtimeParser,
		RawStore:             rawStore,
		Assets:               db.NewDocumentAssetRepository(gormDB),
		MaxFileSizeBytes:     cfg.Ingest.MaxFileSizeBytes,
		MaxChunksPerDocument: cfg.Ingest.MaxChunksPerDocument,
		EmbeddingConcurrency: cfg.Embedding.MaxConcurrency,
		EmbeddingBatchLimit:  cfg.Embedding.BatchSize,
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
	searchRunRepo := db.NewSearchRunRepository(gormDB)
	searchRunRetention := cfg.Retrieval.SearchRunRetention
	if searchRunRetention <= 0 {
		searchRunRetention = service.DefaultSearchRunRetention
	}
	searchRunCleanup := service.NewSearchRunCleanupService(searchRunRepo)
	search := service.NewSearchService(service.SearchServiceDeps{
		Repository: retrievalRepo, Resolver: embeddingResolver, RerankResolver: rerankResolver,
		SearchProfile: workspaceSearchSettings, SearchRuns: searchRunRepo,
		SearchRunRetention: searchRunRetention, Logger: log,
	})
	apiKeyNameStore := db.NewAPIKeyNameStoreDB(gormDB)
	multiSearch := service.NewMultiKnowledgeSearchService(
		retrievalRepo, embeddingResolver, rerankResolver, workspaceSearchSettings,
		apiKeyNameStore, cfg.Search, log, searchRunRepo, searchRunRetention,
	)
	searchReplay := service.NewSearchReplayService(service.SearchReplayDeps{
		Runs: searchRunRepo, Repository: retrievalRepo, Resolver: embeddingResolver,
		RerankResolver: rerankResolver, SearchProfile: workspaceSearchSettings,
		Logger: log, SearchRunRetention: searchRunRetention,
	})
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
	// 同一 repository 同时供 CRUD service 与同步 selector 复用。
	sourceConnectionRepo := db.NewSourceConnectionRepository(gormDB)
	sourceConnectionService := service.NewSourceConnectionService(service.SourceConnectionServiceDeps{
		Repository: sourceConnectionRepo, Cipher: sourceConnectionCipher,
	})
	sourceConnectionSelector := service.NewSourceConnectionSelector(
		sourceConnectionRepo, sourceConnectionCipher,
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
		MaxContentBytes:         cfg.SourceSync.MaxContentBytes,
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

	// 来源对象清理：service（幂等删除 raw/parser/asset）+ scheduler（恢复 pending 孤儿）。
	sourceCleanupStore := db.NewSourceCleanupStore(gormDB)
	sourceCleanup := service.NewSourceCleanupService(service.SourceCleanupServiceDeps{
		Store:      sourceCleanupStore,
		RawStore:   rawStore,
		AssetStore: assetStore,
		Logger:     log,
	})
	sourceCleanupScheduler := service.NewSourceCleanupScheduler(service.SourceCleanupSchedulerDeps{
		Store:    sourceCleanupStore,
		Queue:    jobQueue,
		Interval: time.Duration(cfg.SourceSync.SchedulerIntervalSeconds) * time.Second,
		Logger:   log,
	})

	return &runtimeServices{
		userRepo:         userRepo,
		sessionRepo:      sessionRepo,
		membershipRepo:   membershipRepo,
		invitationRepo:   invitationRepo,
		users:            users,
		auth:             auth,
		invitations:      invitations,
		memberships:      memberships,
		sessionCfg:       cfg.Auth.Session,
		publicURLs:       publicURLs,
		oidc:             oidcLogin,
		oidcAcceptor:     oidcAcceptor,
		oidcEnabled:      cfg.Auth.OIDC.Enabled,
		passwordEnabled:  cfg.Auth.Password.Enabled,
		memoryStateStore: memoryStateStore,

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
		workspaces:              service.NewWorkspaceService(wsRepo, cfg.Auth.OIDC.Enabled),
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
		documentIngest:          newDocumentIngestService(gormDB, rawStore, jobQueue, cfg),
		documentRetry: service.NewDocumentRetryService(service.DocumentRetryServiceDeps{
			Store:  db.NewDocumentRetryStore(gormDB),
			Queue:  jobQueue,
			Logger: log,
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
		sourceCleanup:          sourceCleanup,
		sourceCleanupScheduler: sourceCleanupScheduler,
		sourceConnections:      sourceConnectionService,
		search:                 search,
		multiSearch:            multiSearch,
		searchReplay:           searchReplay,
		retrievalCleanup:       retrievalCleanup,
		searchRunCleanup:       searchRunCleanup,
		pipeline:               documentPipeline,
		rawStore:               rawStore,
		parserRegistry:         runtimeParser,
		assetStore:             assetStore,
		maxFileSize:            cfg.Ingest.MaxFileSizeBytes,
		apiKeys:                apiKeys,
		mcpInlineLimit:         cfg.MCP.InlineIngestMaxFileSizeBytes,
		mcpHostProtection:      cfg.Server.MCPHostProtection,
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

// newDocumentIngestService wires the File-ingest service with its persistence
// store. The same DocumentIngestDBStore serves as both the ingest transaction
// store and the idempotency replay store (it implements both interfaces).
func newDocumentIngestService(gormDB *gorm.DB, rawStore storageport.RawDocumentStore, jobQueue queueport.JobQueue, cfg *config.Config) *service.DocumentIngestService {
	store := db.NewDocumentIngestDBStore(gormDB)
	return service.NewDocumentIngestService(service.DocumentIngestServiceDeps{
		Store:            store,
		ReplayStore:      store,
		RawStore:         rawStore,
		Queue:            jobQueue,
		AllowedFileTypes: cfg.Ingest.AllowedFileTypes,
	})
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
		DocumentDelete:            langmcp.NewMCPDocumentDeleteService(services.documents),
		ChunkGet:                  langmcp.NewMCPChunkGetService(services.chunkRevisions),
		DocumentRetry:             services.documentRetry,
		MultiSearch:               services.multiSearch,
		InlineLimit:               services.mcpInlineLimit,
		EnableLocalhostProtection: services.mcpHostProtection,
	})
	// 避免 typed-nil interface 陷阱：services.oidc 是 *OIDCLoginService(nil)，
	// 直接赋给 Dependencies.OIDC（interface）会产生 typed-nil（!= nil 但底层 nil），
	// 导致 router 误注册 OIDC 路由、前端调用时 panic。nil 时保持 interface 为 nil。
	var oidcDep langhttp.OIDCLoginServiceHTTP
	if services.oidc != nil {
		oidcDep = services.oidc
	}
	return langhttp.NewRouter(langhttp.Dependencies{
		// auth (Task 8)
		Auth:            services.auth,
		Users:           services.users,
		Invitations:     services.invitations,
		Memberships:     services.memberships,
		SessionConfig:   services.sessionCfg,
		PublicURLs:      services.publicURLs,
		APIKeys:         services.apiKeys,
		APIKeyAuth:      services.apiKeys,
		OIDC:            oidcDep,
		OIDCAcceptor:    services.oidcAcceptor,
		OIDCCompleter:   services.oidcAcceptor,
		OIDCEnabled:     services.oidcEnabled,
		PasswordEnabled: services.passwordEnabled,

		// resource (workspace-scoped)
		Workspaces:                services.workspaces,
		WorkspaceReadiness:        services.workspaceReadiness,
		WorkspaceSearchSettings:   services.workspaceSearchSettings,
		KnowledgeBaseSummary:      services.knowledgeBaseSummary,
		KnowledgeBases:            services.knowledgeBases,
		KnowledgeBaseSync:         &httpKnowledgeBaseSyncService{sync: services.sourceSync},
		KnowledgeBaseSourcePolicy: services.knowledgeBases,
		ModelProviders:            services.modelProviders,
		Models:                    services.models,
		ModelConnectionTests:      services.modelConnectionTests,
		DocumentIngest:            services.documentIngest,
		Documents:                 services.documents,
		DocumentRetry:             services.documentRetry,
		DocumentAssets:            services.documentAssets,
		AssetGetter:               services.documentAssets,
		AssetContentStore:         services.assetStore,
		FAQDocuments:              services.faqDocuments,
		FileTree:                  services.fileTree,
		ChunkRevisions:            services.chunkRevisions,
		DocumentChunks:            services.documentChunks,
		IndexGenerations:          services.indexGenerations,
		Search:                    services.search,
		MultiSearch:               services.multiSearch,
		SearchReplay:              services.searchReplay,
		Jobs:                      services.jobs,
		SourceConnections:         services.sourceConnections,
		MCPHandler:                mcpServer.Handler(),
		SPA:                       webspa.SPA,
		MaxFileSizeBytes:          services.maxFileSize,
		ReadyChecker:              services.readiness,
		MetricsPath:               services.metricsPath,
		HTTPMetrics:               services.httpMetrics,
		QueueAdmin:                services.queueAdmin,
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
	asynqWorkerActive := a.workerServer != nil && a.workerMux != nil
	memoryWorkerActive := a.memoryQueue != nil || a.sqliteQueue != nil
	wantReady := 0
	if asynqWorkerActive || memoryWorkerActive {
		wantReady++
	}
	if a.httpServer != nil {
		wantReady++
	}
	readyCh := make(chan struct{}, wantReady)

	if asynqWorkerActive {
		log.Info("启动 asynq worker")
		// Start 同步返回即代表 worker 已启动（asynq 输出 Starting processing 并
		// 拉起全部子组件）；退出时由 app.shutdown 调用 workerServer.Shutdown 优雅关闭。
		if err := a.workerServer.Start(a.workerMux); err != nil {
			return fmt.Errorf("启动 asynq worker 失败: %w", err)
		}
		a.startWorkerBackgroundJobs(ctx, log)
		readyCh <- struct{}{}
	} else if memoryWorkerActive {
		// 无 Redis 队列 worker（内存或 SQLite 持久化）：
		// 在进程内启动 goroutine 消费 pending，复用同一 workerMux 执行 handler。
		log.Info("启动队列 worker")
		if a.memoryQueue != nil {
			a.memoryQueue.Start(ctx)
		}
		if a.sqliteQueue != nil {
			a.sqliteQueue.Start(ctx)
		}
		a.startWorkerBackgroundJobs(ctx, log)
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

// startWorkerBackgroundJobs 启动随 worker 生命周期运行的后台 scheduler / 清理循环。
// asynq worker 与内存队列 worker 共用本方法：ctx 取消即随 shutdown 停止。
func (a *appRuntime) startWorkerBackgroundJobs(ctx context.Context, log *slog.Logger) {
	if a.services == nil {
		return
	}
	// 来源同步 Meta Scheduler：周期扫描到期飞书 KB，按来源连接限流入队。
	if a.services.sourceSyncScheduler != nil {
		go func() { _ = a.services.sourceSyncScheduler.Run(ctx) }()
	}
	// 来源对象清理 Scheduler：启动时 RequeuePending 恢复孤儿 Job，随后周期 Tick 重派。
	if a.services.sourceCleanupScheduler != nil {
		go func() {
			if err := a.services.sourceCleanupScheduler.RequeuePending(ctx); err != nil && ctx.Err() == nil {
				log.Warn("source_cleanup scheduler 启动恢复失败", "error", err.Error())
			}
			_ = a.services.sourceCleanupScheduler.Run(ctx)
		}()
	}
	// Retrieval 投影定时清理：周期性删除过期的 staging/failed/retired 投影，
	// 避免 rebuildable 数据无限增长。
	if a.services.retrievalCleanup != nil && a.cfg.Retrieval.CleanupIntervalSeconds > 0 {
		interval := time.Duration(a.cfg.Retrieval.CleanupIntervalSeconds) * time.Second
		go runRetrievalCleanupLoop(ctx, a.services.retrievalCleanup, a.services.searchRunCleanup, a.cfg.Retrieval.CleanupBatchSize, interval, log)
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
	if a.memoryQueue != nil {
		// 内存队列：等待运行中任务到 ctx 超时。
		capture(a.memoryQueue.Stop(ctx))
	}
	if a.sqliteQueue != nil {
		// SQLite 持久化队列：等待运行中任务到 ctx 超时。
		capture(a.sqliteQueue.Stop(ctx))
	}
	if a.memoryStateStore != nil {
		// 内存 OIDC state store：停止后台清理 goroutine（可多次调用）。
		a.memoryStateStore.Close()
	}
	if a.workerServer != nil {
		a.workerServer.Shutdown()
	}
	if a.asynqClient != nil {
		capture(a.asynqClient.Close())
	}
	if a.queueInspector != nil {
		capture(a.queueInspector.Close())
	}
	if a.otelProviders != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		capture(a.otelProviders.Shutdown(shutdownCtx))
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

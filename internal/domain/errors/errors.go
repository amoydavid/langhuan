package errors

import stderrors "errors"

var (
	ErrNotFound                       = stderrors.New("资源不存在")
	ErrValidation                     = stderrors.New("参数校验失败")
	ErrUnauthorized                   = stderrors.New("未认证")
	ErrForbidden                      = stderrors.New("无权限")
	ErrConflict                       = stderrors.New("资源冲突")
	ErrWorkspaceLimitReached          = stderrors.New("单租户模式下仅允许一个 workspace")
	ErrRateLimited                    = stderrors.New("请求过于频繁")
	ErrPasswordLoginDisabled          = stderrors.New("密码登录已关闭")
	ErrPasswordRegistrationDisabled   = stderrors.New("密码注册已关闭")
	ErrUnsupportedFileType            = stderrors.New("不支持的文件类型")
	ErrUnsupportedProvider            = stderrors.New("不支持的 Provider")
	ErrProviderScopeNotAllowed        = stderrors.New("Provider 不允许用于此作用域")
	ErrInvalidProviderConfig          = stderrors.New("Provider 配置无效")
	ErrCredentialsRequired            = stderrors.New("需要配置凭证")
	ErrUnsupportedModelType           = stderrors.New("不支持的模型类型")
	ErrUnsupportedEmbeddingDimension  = stderrors.New("不支持的 Embedding 维度")
	ErrModelNotVisible                = stderrors.New("模型不可见")
	ErrModelDisabled                  = stderrors.New("模型已停用")
	ErrProviderDisabled               = stderrors.New("Provider 已停用")
	ErrDimensionMismatch              = stderrors.New("Embedding 实际维度不匹配")
	ErrConnectionTestFailed           = stderrors.New("连接测试失败")
	ErrCredentialDecryption           = stderrors.New("凭证解密失败")
	ErrImmutableModelField            = stderrors.New("模型语义字段不可修改")
	ErrModelInUse                     = stderrors.New("模型正在使用")
	ErrProviderInUse                  = stderrors.New("Provider 正在使用")
	ErrAuthenticationFailed           = stderrors.New("供应商认证失败")
	ErrEndpointUnreachable            = stderrors.New("供应商地址不可达")
	ErrRequestTimeout                 = stderrors.New("供应商请求超时")
	ErrProviderRejected               = stderrors.New("供应商拒绝请求")
	ErrCatalogUnavailable             = stderrors.New("Provider 模型目录暂不可用")
	ErrInvalidEmbeddingResponse       = stderrors.New("供应商返回了无效向量")
	ErrRevisionConflict               = stderrors.New("修订版本冲突")
	ErrGenerationStale                = stderrors.New("索引代次已过期")
	ErrManualEditConfirmationRequired = stderrors.New("需要确认归档人工编辑")
	ErrGenerationNotReady             = stderrors.New("索引代次尚未就绪")
	ErrGenerationBuildInProgress      = stderrors.New("已有索引代次正在构建")
	ErrFAQChunkImmutable              = stderrors.New("FAQ 分块不可直接修改")
	ErrFileTreeCycle                  = stderrors.New("文件树移动会形成环")
	ErrFileTreeNotEmpty               = stderrors.New("目录非空")
	ErrFileTreeNameConflict           = stderrors.New("同级名称冲突")

	// Workspace API Key 相关领域错误。HTTP 层据此映射稳定错误码。
	ErrAPIKeyInvalidFormat     = stderrors.New("API Key 格式无效")
	ErrAPIKeySecretUnavailable = stderrors.New("API Key 明文不可恢复")
	ErrAPIKeyLimitReached      = stderrors.New("活跃 API Key 数量已达上限")
	ErrInsufficientScope       = stderrors.New("API Key 权限不足")
	ErrAPIKeyImmutable         = stderrors.New("API Key 已吊销，不可修改")

	// Rerank 相关领域错误。HTTP 层据此映射稳定错误码。
	ErrInvalidRerankResponse       = stderrors.New("供应商返回了无效重排结果")
	ErrRerankUnavailable           = stderrors.New("重排服务暂时不可用")
	ErrRerankRateLimited           = stderrors.New("重排服务请求过于频繁")
	ErrRerankInputTooLarge         = stderrors.New("重排输入超过模型限制")
	ErrRerankConfigurationConflict = stderrors.New("所选知识库的重排配置不一致")
	ErrRerankSnapshotMismatch      = stderrors.New("重排模型配置与索引快照不一致")
	ErrEmbeddingSnapshotMismatch   = stderrors.New("Embedding 模型配置与索引快照不一致")

	// ErrIdempotencyConflict 表示同一 Idempotency-Key 携带了不同的请求体，统一映射为 409。
	ErrIdempotencyConflict = stderrors.New("幂等键与已有请求冲突")

	// ErrNotRetryable 表示目标资源当前不在可重试状态（如 revision 非 failed）。
	// 用于失败重试入口，统一映射为 409。
	ErrNotRetryable = stderrors.New("当前状态不可重试")

	// ErrSearchQueryMismatch 表示回放提交的 query 与原 SearchRun 记录的 query_hash 不一致。
	ErrSearchQueryMismatch = stderrors.New("回放 query 与原始检索不一致")

	// ErrGenerationNotAvailable 表示回放所需的 Generation 或其 published projection 已被清理。
	ErrGenerationNotAvailable = stderrors.New("索引代次或其投影已不可用")
)

package main

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// evalConfig 是 langhuan-eval run 的独立配置（eval.config.yaml，不进 git，
// 由 eval.config.example.yaml 提供模板）。
type evalConfig struct {
	Server    evalServerConfig    `yaml:"server"`
	Embedding evalEmbeddingConfig `yaml:"embedding"`
	Rerank    *evalRerankConfig   `yaml:"rerank"`
	Matrix    evalMatrixConfig    `yaml:"matrix"`
	Dataset   evalDatasetConfig   `yaml:"dataset"`
	Overlap   evalOverlapConfig   `yaml:"overlap"`
	Output    evalOutputConfig    `yaml:"output"`
	HF        evalHFConfig        `yaml:"hf"`
}

type evalServerConfig struct {
	// standalone（默认）：评测自带起一个临时琅嬛实例；remote：连接 base_url。
	Mode string `yaml:"mode"`
	// remote 模式的目标地址，如 http://127.0.0.1:8080。
	BaseURL string `yaml:"base_url"`
	// 预构建的 langhuan 二进制；空则用 go build ./cmd/langhuan 现场构建。
	Binary string `yaml:"binary"`
	// standalone 实例监听地址；留空自动分配 127.0.0.1 随机端口。
	HTTPAddr string `yaml:"http_addr"`
	Email    string `yaml:"email"`
	Nickname string `yaml:"nickname"`
	Password string `yaml:"password"`
	// 导入并发（SQLite 单写者，默认 4）。
	IngestConcurrency int `yaml:"ingest_concurrency"`
	// 单文档 ready 等待上限。
	ReadyTimeoutSeconds int `yaml:"ready_timeout_seconds"`
}

type evalEmbeddingConfig struct {
	Provider       string         `yaml:"provider"`
	ProviderConfig map[string]any `yaml:"config"`
	Credentials    map[string]any `yaml:"credentials"`
	// api_key_file 非空时覆盖 credentials.api_key（避免密钥落盘明文配置）。
	APIKeyFile string         `yaml:"api_key_file"`
	ModelName  string         `yaml:"model_name"`
	Dimensions int            `yaml:"dimensions"`
	Parameters map[string]any `yaml:"parameters"`
}

type evalRerankConfig struct {
	Enabled        bool           `yaml:"enabled"`
	Provider       string         `yaml:"provider"`
	ProviderConfig map[string]any `yaml:"config"`
	Credentials    map[string]any `yaml:"credentials"`
	APIKeyFile     string         `yaml:"api_key_file"`
	ModelName      string         `yaml:"model_name"`
	Parameters     map[string]any `yaml:"parameters"`
	CandidateTopK  int            `yaml:"candidate_top_k"`
}

type evalMatrixConfig struct {
	TopK      int `yaml:"top_k"`
	FinalTopK int `yaml:"final_top_k"`
	// 关闭部分通道组合（默认四格全开）。
	SkipVectorOnly bool `yaml:"skip_vector_only"`
	SkipFTSOnly    bool `yaml:"skip_fts_only"`
	SkipHybrid     bool `yaml:"skip_hybrid"`
	SkipRerank     bool `yaml:"skip_rerank"`
}

type evalDatasetConfig struct {
	Dir string `yaml:"dir"`
}

type evalOverlapConfig struct {
	Threshold             float64   `yaml:"threshold"`
	SensitivityThresholds []float64 `yaml:"sensitivity_thresholds"`
}

type evalOutputConfig struct {
	Dir string `yaml:"dir"`
}

type evalHFConfig struct {
	Mirror   string `yaml:"mirror"`
	Fallback string `yaml:"fallback"`
}

func defaultEvalConfig() evalConfig {
	return evalConfig{
		Server: evalServerConfig{
			Mode: "standalone", Email: "eval@langhuan.local", Nickname: "Eval",
			Password: "LanghuanEval!2026", IngestConcurrency: 4, ReadyTimeoutSeconds: 300,
		},
		Embedding: evalEmbeddingConfig{
			Provider: "openai", ProviderConfig: map[string]any{"mode": "standard", "timeout_seconds": 60},
			ModelName: "bge-m3", Dimensions: 1024, Parameters: map[string]any{"batch_size": 32},
		},
		Matrix:  evalMatrixConfig{TopK: 50, FinalTopK: 10},
		Dataset: evalDatasetConfig{Dir: ".eval-data/miracl-zh"},
		Overlap: evalOverlapConfig{Threshold: 0.6, SensitivityThresholds: []float64{0.5, 0.6, 0.8}},
		Output:  evalOutputConfig{Dir: "docs/eval"},
		HF:      evalHFConfig{Mirror: "https://hf-mirror.com", Fallback: "https://huggingface.co"},
	}
}

func loadEvalConfig(path string) (evalConfig, error) {
	config := defaultEvalConfig()
	if path == "" {
		return config, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("读取评测配置失败: %w", err)
	}
	if err := yaml.Unmarshal(body, &config); err != nil {
		return config, fmt.Errorf("解析评测配置失败: %w", err)
	}
	if err := config.validate(); err != nil {
		return config, err
	}
	return config, nil
}

func (c *evalConfig) validate() error {
	if c.Embedding.ModelName == "" || c.Embedding.Dimensions <= 0 {
		return fmt.Errorf("embedding.model_name 与 dimensions 必填")
	}
	switch c.Server.Mode {
	case "", "standalone", "remote":
	default:
		return fmt.Errorf("server.mode 只支持 standalone/remote，当前 %q", c.Server.Mode)
	}
	if c.Server.Mode == "remote" && c.Server.BaseURL == "" {
		return fmt.Errorf("remote 模式必须提供 server.base_url")
	}
	if c.Matrix.TopK <= 0 || c.Matrix.FinalTopK <= 0 {
		return fmt.Errorf("matrix.top_k 与 matrix.final_top_k 必须为正")
	}
	return nil
}

// applyAPIKeyFiles 把 api_key_file 的内容注入 credentials.api_key，
// 让密钥可以只存在于独立文件而不进 eval.config.yaml。
func (c *evalConfig) applyAPIKeyFiles() error {
	if err := injectAPIKey(&c.Embedding.Credentials, c.Embedding.APIKeyFile); err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	if c.Rerank != nil {
		if err := injectAPIKey(&c.Rerank.Credentials, c.Rerank.APIKeyFile); err != nil {
			return fmt.Errorf("rerank: %w", err)
		}
	}
	return nil
}

func injectAPIKey(credentials *map[string]any, keyFile string) error {
	if keyFile == "" {
		return nil
	}
	body, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("读取 api_key_file 失败: %w", err)
	}
	key := string(bytes.TrimSpace(body))
	if key == "" {
		return fmt.Errorf("api_key_file %s 内容为空", keyFile)
	}
	if *credentials == nil {
		*credentials = map[string]any{}
	}
	(*credentials)["api_key"] = key
	return nil
}

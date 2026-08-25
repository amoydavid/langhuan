package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type thresholdMetric struct {
	Threshold float64        `json:"threshold"`
	Metrics   metricsSummary `json:"metrics"`
}

type comboReport struct {
	Name              string            `json:"name"`
	Available         bool              `json:"available"`
	UnavailableReason string            `json:"unavailable_reason,omitempty"`
	ByThreshold       []thresholdMetric `json:"by_threshold,omitempty"`
	// Attribution 拆分主阈值下的未命中（gold 文档已召回 vs 未召回）。
	Attribution *missAttribution `json:"miss_attribution,omitempty"`
}

// metricAt 返回该组合在指定阈值下的指标；缺失时回退首档，保证报告不空。
func (c comboReport) metricAt(threshold float64) metricsSummary {
	for _, item := range c.ByThreshold {
		if item.Threshold == threshold {
			return item.Metrics
		}
	}
	if len(c.ByThreshold) > 0 {
		return c.ByThreshold[0].Metrics
	}
	return metricsSummary{}
}

type trackReport struct {
	Name       string        `json:"name"`
	Label      string        `json:"label"`
	CorpusSize int           `json:"corpus_size"`
	QueryCount int           `json:"query_count"`
	Combos     []comboReport `json:"combos"`
}

type reportFingerprint struct {
	GeneratedAt      string         `json:"generated_at"`
	Dataset          string         `json:"dataset"`
	DatasetDir       string         `json:"dataset_dir"`
	ManifestSHA256   string         `json:"manifest_sha256"`
	Seed             int64          `json:"seed"`
	QueryCount       int            `json:"query_count"`
	TrackACorpusSize int            `json:"track_a_corpus_size"`
	TrackBCorpusSize int            `json:"track_b_corpus_size"`
	Chunking         map[string]any `json:"chunking"`
	Embedding        map[string]any `json:"embedding"`
	Rerank           map[string]any `json:"rerank"`
	Matrix           map[string]any `json:"matrix"`
	OverlapThreshold float64        `json:"overlap_threshold"`
	RepoHead         string         `json:"repo_head"`
	ServerBaseURL    string         `json:"server_base_url"`
	ServerMode       string         `json:"server_mode"`
}

type reportDocument struct {
	Fingerprint reportFingerprint `json:"fingerprint"`
	Tracks      []trackReport     `json:"tracks"`
}

// writeReport 输出 metrics.json（机器可 diff）与 report.md（人读），
// 目录名带数据集指纹与模型名，保证不同 run 不互相覆盖（spec §9）。
func writeReport(cfg evalConfig, dataset *evalDataset, baseURL, repoRoot string, tracks []trackReport) error {
	manifestSum, err := fileSHA256(filepath.Join(dataset.Dir, "manifest.json"))
	if err != nil {
		return err
	}
	fingerprint := reportFingerprint{
		GeneratedAt: time.Now().Format(time.RFC3339), Dataset: dataset.Manifest.Dataset,
		// dataset_dir 用仓库相对路径：报告会提交进 git，指纹不得携带
		// 本地绝对路径（用户名等环境信息）。
		DatasetDir:     relPathFromRoot(dataset.Dir, repoRoot),
		ManifestSHA256: manifestSum, Seed: dataset.Manifest.Seed,
		QueryCount:       len(dataset.Queries),
		TrackACorpusSize: dataset.Manifest.TrackACorpusSize, TrackBCorpusSize: dataset.Manifest.TrackBCorpusSize,
		Chunking: chunkingFingerprint(cfg),
		Embedding: map[string]any{
			"provider": cfg.Embedding.Provider, "model_name": cfg.Embedding.ModelName,
			"dimensions": cfg.Embedding.Dimensions, "parameters": cfg.Embedding.Parameters,
		},
		Matrix: map[string]any{
			"top_k": cfg.Matrix.TopK, "final_top_k": cfg.Matrix.FinalTopK,
		},
		OverlapThreshold: cfg.Overlap.Threshold,
		RepoHead:         repoHead(repoRoot), ServerBaseURL: baseURL, ServerMode: cfg.Server.Mode,
	}
	if cfg.Rerank != nil && cfg.Rerank.Enabled {
		fingerprint.Rerank = map[string]any{
			"model_name": cfg.Rerank.ModelName, "candidate_top_k": cfg.Rerank.CandidateTopK,
		}
	} else {
		fingerprint.Rerank = map[string]any{"enabled": false}
	}

	runDir := filepath.Join(
		cfg.Output.Dir,
		fmt.Sprintf("%s_%s_%s", time.Now().Format("20060102-150405"), dataset.Manifest.Dataset, sanitizeName(cfg.Embedding.ModelName)),
	)
	if !filepath.IsAbs(runDir) {
		runDir = filepath.Join(repoRoot, runDir)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	document := reportDocument{Fingerprint: fingerprint, Tracks: tracks}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "metrics.json"), body, 0o644); err != nil {
		return err
	}
	markdown := renderMarkdown(document, cfg)
	if err := os.WriteFile(filepath.Join(runDir, "report.md"), []byte(markdown), 0o644); err != nil {
		return err
	}
	fmt.Printf("\n报告已生成：\n  %s\n  %s\n", filepath.Join(runDir, "report.md"), filepath.Join(runDir, "metrics.json"))
	return nil
}

// chunkingFingerprint 反映评测实际使用的分块配置；未覆盖时为生产默认
// （chunker v3：auto 父子 4096/384）。
func chunkingFingerprint(cfg evalConfig) map[string]any {
	chunking := map[string]any{
		"strategy": "auto", "enable_parent_child": true,
		"parent_chunk_size": 4096, "child_chunk_size": 384, "chunker_version": 3,
	}
	if cfg.Chunking != nil {
		if cfg.Chunking.Strategy != "" {
			chunking["strategy"] = cfg.Chunking.Strategy
		}
		if cfg.Chunking.ParentChunkSize > 0 {
			chunking["parent_chunk_size"] = cfg.Chunking.ParentChunkSize
		}
		if cfg.Chunking.ChildChunkSize > 0 {
			chunking["child_chunk_size"] = cfg.Chunking.ChildChunkSize
		}
		if cfg.Chunking.EnableParentChild != nil {
			chunking["enable_parent_child"] = *cfg.Chunking.EnableParentChild
		}
		if cfg.Chunking.ChunkOverlap > 0 {
			chunking["chunk_overlap"] = cfg.Chunking.ChunkOverlap
		}
		chunking["note"] = "eval 配置覆盖"
	}
	return chunking
}

// relPathFromRoot 把绝对路径转成相对 root 的路径（报告指纹脱敏用）；
// 无法求相对（跨卷等）时退回路径末段，保证不泄漏本地前缀。
func relPathFromRoot(path, root string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(path)
}

func sanitizeName(name string) string {
	safe := regexp.MustCompile(`[^\w.-]+`).ReplaceAllString(name, "-")
	return strings.Trim(safe, "-")
}

func renderMarkdown(document reportDocument, cfg evalConfig) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 离线检索评测报告：%s\n\n", document.Fingerprint.Dataset)
	builder.WriteString("## 指纹（复现所需全部信息）\n\n")
	fmt.Fprintf(&builder, "| 字段 | 值 |\n|---|---|\n")
	fmt.Fprintf(&builder, "| 生成时间 | %s |\n", document.Fingerprint.GeneratedAt)
	fmt.Fprintf(&builder, "| 数据集 | %s（seed=%d, manifest=%s…） |\n",
		document.Fingerprint.Dataset, document.Fingerprint.Seed, shortSHA(document.Fingerprint.ManifestSHA256))
	fmt.Fprintf(&builder, "| query / TrackA 语料 / TrackB 语料 | %d / %d / %d |\n",
		document.Fingerprint.QueryCount, document.Fingerprint.TrackACorpusSize, document.Fingerprint.TrackBCorpusSize)
	fmt.Fprintf(&builder, "| 分块 | %v |\n", chunkingSummary(document.Fingerprint.Chunking))
	fmt.Fprintf(&builder, "| Embedding | %s/%s dim=%d |\n",
		document.Fingerprint.Embedding["provider"], document.Fingerprint.Embedding["model_name"],
		document.Fingerprint.Embedding["dimensions"])
	if model, ok := document.Fingerprint.Rerank["model_name"]; ok && model != nil {
		fmt.Fprintf(&builder, "| Rerank | %v |\n", model)
	} else {
		builder.WriteString("| Rerank | 未启用 |\n")
	}
	fmt.Fprintf(&builder, "| 通道矩阵 | top_k=%v final_top_k=%v |\n",
		document.Fingerprint.Matrix["top_k"], document.Fingerprint.Matrix["final_top_k"])
	fmt.Fprintf(&builder, "| 命中阈值 | %.2f（敏感性见各组合明细） |\n", document.Fingerprint.OverlapThreshold)
	fmt.Fprintf(&builder, "| 琅嬛仓库 | %s |\n", shortSHA(document.Fingerprint.RepoHead))
	fmt.Fprintf(&builder, "| 被测实例 | %s（%s） |\n\n", document.Fingerprint.ServerMode, document.Fingerprint.ServerBaseURL)

	for _, track := range document.Tracks {
		fmt.Fprintf(&builder, "## %s：%s\n\n", track.Name, track.Label)
		fmt.Fprintf(&builder, "语料 %d 份 / query %d 条。\n\n", track.CorpusSize, track.QueryCount)
		builder.WriteString("| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |\n|---|---|---|---|---|---|\n")
		for _, combo := range track.Combos {
			if !combo.Available {
				fmt.Fprintf(&builder, "| %s | - | - | - | - | N/A（%s） |\n", combo.Name, combo.UnavailableReason)
				continue
			}
			primary := combo.metricAt(cfg.Overlap.Threshold)
			fmt.Fprintf(&builder, "| %s | %.4f | %.4f | %.4f | %.4f | OK |\n",
				combo.Name, primary.RecallAt5, primary.RecallAt10, primary.MRRAt10, primary.NDCGAt10)
		}
		builder.WriteString("\n阈值敏感性（ndcg@10）:\n\n")
		builder.WriteString("| 通道组合 ")
		thresholds := collectThresholds(track)
		for _, threshold := range thresholds {
			fmt.Fprintf(&builder, "| @%.2f ", threshold)
		}
		builder.WriteString("|\n|---")
		for range thresholds {
			builder.WriteString("|---")
		}
		builder.WriteString("|\n")
		for _, combo := range track.Combos {
			if !combo.Available {
				continue
			}
			fmt.Fprintf(&builder, "| %s ", combo.Name)
			for _, threshold := range thresholds {
				fmt.Fprintf(&builder, "| %.4f ", combo.metricAt(threshold).NDCGAt10)
			}
			builder.WriteString("|\n")
		}
		builder.WriteString("\n未命中归因（主阈值）:\n\n")
		builder.WriteString("| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |\n|---|---|---|---|\n")
		for _, combo := range track.Combos {
			if !combo.Available || combo.Attribution == nil {
				continue
			}
			fmt.Fprintf(&builder, "| %s | %d | %d | %d |\n",
				combo.Name, combo.Attribution.Missed, combo.Attribution.MissedDocRecalled, combo.Attribution.MissedDocNotRecalled)
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## 如何解读\n\n")
	builder.WriteString("- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，" +
		"是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。\n")
	builder.WriteString("- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，" +
		"track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。\n")
	builder.WriteString("- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。\n")
	builder.WriteString("- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、" +
		"矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。\n")
	builder.WriteString("- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，" +
		"先校准阈值再下结论。\n")
	return builder.String()
}

func collectThresholds(track trackReport) []float64 {
	seen := map[float64]struct{}{}
	for _, combo := range track.Combos {
		for _, item := range combo.ByThreshold {
			seen[item.Threshold] = struct{}{}
		}
	}
	list := make([]float64, 0, len(seen))
	for threshold := range seen {
		list = append(list, threshold)
	}
	sort.Float64s(list)
	return list
}

func chunkingSummary(chunking map[string]any) string {
	return fmt.Sprintf("%v parent=%v child=%v",
		chunking["strategy"], chunking["parent_chunk_size"], chunking["child_chunk_size"])
}

func shortSHA(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// runEval 是 langhuan-eval run 的主流程（spec §7.2）：拉起被测系统 →
// REST 引导 → 双轨导入 → 通道矩阵检索 → 指标 → 报告。
// matrixCombo 是通道矩阵的一个格子（spec §6.3）：topK=0 表示禁用该路。
type matrixCombo struct {
	Name        string
	VectorTopK  int
	KeywordTopK int
	Rerank      bool
	Skip        bool
}

func matrixCombos(cfg evalConfig, rerankEnabled bool) []matrixCombo {
	return []matrixCombo{
		{Name: "vector_only", VectorTopK: cfg.Matrix.TopK, KeywordTopK: 0, Skip: cfg.Matrix.SkipVectorOnly},
		{Name: "fts_only", VectorTopK: 0, KeywordTopK: cfg.Matrix.TopK, Skip: cfg.Matrix.SkipFTSOnly},
		{Name: "hybrid", VectorTopK: cfg.Matrix.TopK, KeywordTopK: cfg.Matrix.TopK, Skip: cfg.Matrix.SkipHybrid},
		{Name: "hybrid_rerank", VectorTopK: cfg.Matrix.TopK, KeywordTopK: cfg.Matrix.TopK, Rerank: true, Skip: cfg.Matrix.SkipRerank},
	}
}

func runEval(configPath string) error {
	cfg, err := loadEvalConfig(configPath)
	if err != nil {
		return err
	}
	if err := cfg.applyAPIKeyFiles(); err != nil {
		return err
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	datasetPath := cfg.Dataset.Dir
	if !filepath.IsAbs(datasetPath) {
		datasetPath = filepath.Join(repoRoot, datasetPath)
	}
	dataset, err := loadDataset(datasetPath)
	if err != nil {
		return err
	}
	fmt.Printf("数据集：%s（query=%d TrackA=%d TrackB=%d seed=%d）\n",
		datasetPath, len(dataset.Queries), len(dataset.TrackA), len(dataset.TrackB), dataset.Manifest.Seed)

	var baseURL string
	var server *standaloneServer
	if cfg.Server.Mode == "remote" {
		baseURL = strings.TrimRight(cfg.Server.BaseURL, "/")
		fmt.Printf("remote 模式：%s\n", baseURL)
	} else {
		fmt.Println("standalone 模式：拉起临时琅嬛实例（SQLite）…")
		server, err = startStandaloneServer(cfg, repoRoot)
		if err != nil {
			return err
		}
		defer server.stop()
		baseURL = server.baseURL
		fmt.Printf("  就绪：%s（日志 %s）\n", baseURL, filepath.Join(server.dataDir, "server.log"))
	}

	client, err := newLanghuanClient(baseURL)
	if err != nil {
		return err
	}
	fmt.Println("REST 引导：注册用户 / workspace / embedding 模型…")
	boot, err := client.bootstrap(cfg)
	if err != nil {
		return err
	}
	rerankEnabled := cfg.Rerank != nil && cfg.Rerank.Enabled && boot.RerankModelID != ""
	fmt.Printf("  workspace=%s embedding=%s(dim=%d) rerank=%v\n",
		boot.WorkspaceSlug, cfg.Embedding.ModelName, cfg.Embedding.Dimensions, rerankEnabled)

	golds := dataset.goldPassagesByQuery()
	evaluatable := 0
	for _, query := range dataset.Queries {
		if len(golds[query.QueryID]) > 0 {
			evaluatable++
		}
	}
	if evaluatable == 0 {
		return fmt.Errorf("没有可评测 query（gold 段落缺失）")
	}
	fmt.Printf("可评测 query：%d / %d\n", evaluatable, len(dataset.Queries))

	allTracks := []trackSpec{
		{Name: "track-a", Label: trackALabelFor(dataset.Manifest.Dataset), Docs: trackADocsOf(dataset), Slug: boot.WorkspaceSlug, LongDoc: false},
		{Name: "track-b", Label: trackBLabelFor(dataset.Manifest.Dataset), Docs: trackBDocsOf(dataset), Slug: boot.WorkspaceSlug, LongDoc: true},
	}
	var tracks []trackSpec
	for _, track := range allTracks {
		if len(cfg.Tracks) == 0 {
			tracks = append(tracks, track)
			continue
		}
		for _, wanted := range cfg.Tracks {
			if track.Name == wanted {
				tracks = append(tracks, track)
				break
			}
		}
	}
	if len(tracks) == 0 {
		return fmt.Errorf("tracks 过滤后为空（可选值 track-a/track-b）")
	}
	combos := matrixCombos(cfg, rerankEnabled)

	var trackReports []trackReport
	for _, track := range tracks {
		fmt.Printf("\n[%s] 创建知识库并导入 %d 份文档（并发 %d）…\n",
			track.Name, len(track.Docs), cfg.Server.IngestConcurrency)
		kbID, err := client.createKnowledgeBase(boot.WorkspaceSlug, "eval-"+track.Name, boot.EmbeddingModelID, cfg.Chunking)
		if err != nil {
			return err
		}
		started := time.Now()
		if err := ingestAll(client, boot.WorkspaceSlug, kbID, track.Docs, cfg); err != nil {
			return err
		}
		fmt.Printf("  导入完成（%s），执行查询矩阵…\n", time.Since(started).Round(time.Second))

		trackResult := trackReport{
			Name: track.Name, Label: track.Label,
			CorpusSize: len(track.Docs), QueryCount: evaluatable,
			Combos: make([]comboReport, 0, len(combos)),
		}
		for _, combo := range combos {
			if combo.Skip {
				trackResult.Combos = append(trackResult.Combos, comboReport{
					Name: combo.Name, Available: false, UnavailableReason: "配置跳过",
				})
				continue
			}
			if combo.Rerank && !rerankEnabled {
				trackResult.Combos = append(trackResult.Combos, comboReport{
					Name: combo.Name, Available: false, UnavailableReason: "未配置 rerank 模型",
				})
				continue
			}
			if combo.Rerank {
				if err := client.setRerankSettings(boot.WorkspaceSlug, true, boot.RerankModelID, cfg.Rerank.CandidateTopK); err != nil {
					return fmt.Errorf("启用 rerank 失败: %w", err)
				}
			}
			summary, attribution, err := evaluateCombo(client, boot.WorkspaceSlug, kbID, dataset, golds,
				dataset.goldDocTokensByQuery(track.LongDoc), combo, cfg, thresholdList(cfg))
			if combo.Rerank {
				if err := client.setRerankSettings(boot.WorkspaceSlug, false, boot.RerankModelID, cfg.Rerank.CandidateTopK); err != nil {
					return fmt.Errorf("关闭 rerank 失败: %w", err)
				}
			}
			if err != nil {
				return fmt.Errorf("%s/%s 评测失败: %w", track.Name, combo.Name, err)
			}
			fmt.Printf("  %-14s recall@10=%.4f mrr@10=%.4f ndcg@10=%.4f（未命中 %d：文档已召回 %d / 未召回 %d）\n",
				combo.Name, summary[cfg.Overlap.Threshold].RecallAt10,
				summary[cfg.Overlap.Threshold].MRRAt10, summary[cfg.Overlap.Threshold].NDCGAt10,
				attribution.Missed, attribution.MissedDocRecalled, attribution.MissedDocNotRecalled)
			trackResult.Combos = append(trackResult.Combos, comboReport{
				Name: combo.Name, Available: true, ByThreshold: sortedThresholdMetrics(summary),
				Attribution: &attribution,
			})
		}
		trackReports = append(trackReports, trackResult)
	}
	return writeReport(cfg, dataset, baseURL, repoRoot, trackReports)
}

type trackSpec struct {
	Name, Label, Slug string
	Docs              []ingestDoc
	// LongDoc 标记长文档轨道：归因用文章 id（docid '#' 前缀）识别 gold 文档。
	LongDoc bool
}

// trackALabelFor / trackBLabelFor 按数据集给出轨道的人读描述。
func trackALabelFor(dataset string) string {
	if strings.HasPrefix(dataset, vcsumDatasetName) {
		return "话题段检索（单话题段文档，隔离分块变量）"
	}
	return "段落检索（单段落文档，隔离分块变量）"
}

func trackBLabelFor(dataset string) string {
	if strings.HasPrefix(dataset, vcsumDatasetName) {
		return "会议转写长文档检索（无结构连续文本，覆盖分块+父子+检索全链路）"
	}
	return "长文档检索（Wikipedia 文章聚合，覆盖分块+父子+检索全链路）"
}

type ingestDoc struct {
	Title, Content string
}

func trackADocsOf(dataset *evalDataset) []ingestDoc {
	docs := make([]ingestDoc, 0, len(dataset.TrackA))
	for _, doc := range dataset.TrackA {
		docs = append(docs, ingestDoc{Title: uniqueTitle(doc.Title, doc.DocID), Content: doc.Text})
	}
	return docs
}

func trackBDocsOf(dataset *evalDataset) []ingestDoc {
	docs := make([]ingestDoc, 0, len(dataset.TrackB))
	for _, doc := range dataset.TrackB {
		docs = append(docs, ingestDoc{
			Title:   uniqueTitle(doc.Title, doc.DocID),
			Content: strings.Join(doc.Passages, "\n\n"),
		})
	}
	return docs
}

// uniqueTitle 保证文件树同级名称唯一：MIRACL 的段落标题沿用文章标题，
// 同一文章的多个段落（及跨文章同名）会触发 file_tree_name_conflict。
func uniqueTitle(title, docID string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return docID
	}
	return title + " [" + docID + "]"
}

func ingestAll(client *langhuanClient, slug, kbID string, docs []ingestDoc, cfg evalConfig) error {
	workers := cfg.Server.IngestConcurrency
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var failure struct {
		sync.Mutex
		err error
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				doc := docs[index]
				documentID, err := client.ingestText(slug, kbID, doc.Title, doc.Content)
				if err == nil {
					err = client.waitDocumentReady(slug, documentID,
						time.Duration(cfg.Server.ReadyTimeoutSeconds)*time.Second)
				}
				if err != nil {
					failure.Lock()
					if failure.err == nil {
						failure.err = fmt.Errorf("文档 %q 导入失败: %w", doc.Title, err)
					}
					failure.Unlock()
				}
			}
		}()
	}
	// 生产者：顺序派发，任一 worker 失败即停止派发（jobs 关闭后 worker 排空退出）。
	go func() {
		total := len(docs)
		sent := 0
		for index := range docs {
			failure.Lock()
			err := failure.err
			failure.Unlock()
			if err != nil {
				break
			}
			jobs <- index
			sent++
			if sent%500 == 0 {
				fmt.Printf("    已提交 %d/%d\n", sent, total)
			}
		}
		close(jobs)
	}()
	wg.Wait()
	if failure.err != nil {
		return failure.err
	}
	return nil
}

// evaluateCombo 对一个通道组合执行全部 query，返回 threshold -> 指标与
// 未命中归因。每个 query 只检索一次；不同阈值复用同一批结果重新做命中
// 判定。归因在主阈值下计算：未命中的 query 若 gold 文档出现在返回列表
// （按标题里的 [docid] 标记识别）则计为分块/匹配损耗，否则为文档未召回。
func evaluateCombo(
	client *langhuanClient,
	slug, kbID string,
	dataset *evalDataset,
	golds map[string][]string,
	goldDocTokens map[string][]string,
	combo matrixCombo,
	cfg evalConfig,
	thresholds []float64,
) (map[float64]metricsSummary, missAttribution, error) {
	attribution := missAttribution{}
	type rankedResult struct {
		golds     []string
		docTokens []string
		items     []searchResultItem
	}
	results := make([]rankedResult, 0, len(dataset.Queries))
	for _, query := range dataset.Queries {
		gold := golds[query.QueryID]
		if len(gold) == 0 {
			continue
		}
		items, err := client.search(slug, kbID, query.Query, combo.VectorTopK, combo.KeywordTopK, cfg.Matrix.FinalTopK)
		if err != nil {
			return nil, attribution, fmt.Errorf("query %s 检索失败: %w", query.QueryID, err)
		}
		results = append(results, rankedResult{golds: gold, docTokens: goldDocTokens[query.QueryID], items: items})
	}
	summary := make(map[float64]metricsSummary, len(thresholds))
	for _, threshold := range thresholds {
		evals := make([]queryEvaluation, 0, len(results))
		goldCounts := make([]int, 0, len(results))
		for _, result := range results {
			ranks := ranksOf(result.items, result.golds, threshold)
			evals = append(evals, queryEvaluation{Ranks: ranks})
			goldCounts = append(goldCounts, len(result.golds))
			if threshold == cfg.Overlap.Threshold && len(ranks) == 0 {
				attribution.Missed++
				if goldDocumentRecalled(result.items, result.docTokens) {
					attribution.MissedDocRecalled++
				} else {
					attribution.MissedDocNotRecalled++
				}
			}
		}
		summary[threshold] = summarize(evals, goldCounts)
	}
	return summary, attribution, nil
}

// missAttribution 拆分主阈值下的未命中：gold 文档被召回但文本重叠不足
// （分块边界/父块稀释/阈值），还是 gold 文档根本没进返回列表（召回问题）。
type missAttribution struct {
	Missed               int `json:"missed"`
	MissedDocRecalled    int `json:"missed_doc_recalled"`
	MissedDocNotRecalled int `json:"missed_doc_not_recalled"`
}

// goldDocumentRecalled 判断任一 gold 文档是否出现在结果列表。导入标题带
// "[docid]" 后缀（uniqueTitle），Track A 的 docid 是段落 id，Track B 是文章 id。
func goldDocumentRecalled(items []searchResultItem, docTokens []string) bool {
	if len(docTokens) == 0 {
		return false
	}
	for _, item := range items {
		for _, token := range docTokens {
			if strings.Contains(item.DocumentName, "["+token+"]") {
				return true
			}
		}
	}
	return false
}

// sortedThresholdMetrics 把 threshold -> 指标 的 map 转为按阈值升序的切片
// （JSON map 不支持 float 键）。
func sortedThresholdMetrics(summary map[float64]metricsSummary) []thresholdMetric {
	thresholds := make([]float64, 0, len(summary))
	for threshold := range summary {
		thresholds = append(thresholds, threshold)
	}
	sort.Float64s(thresholds)
	result := make([]thresholdMetric, 0, len(thresholds))
	for _, threshold := range thresholds {
		result = append(result, thresholdMetric{Threshold: threshold, Metrics: summary[threshold]})
	}
	return result
}

// ranksOf 计算一次检索的命中排名：按结果顺序，只有当某条结果首次覆盖
// 一个尚未覆盖的 gold 时才记录排名，保证多 gold recall 不重复计数。
func ranksOf(items []searchResultItem, golds []string, threshold float64) []int {
	covered := make([]bool, len(golds))
	var ranks []int
	for position, item := range items {
		// 命中判定只用父块正文：命中子块正文构造上是父块的子串（chunker 父子
		// 装配即拼接），v1.2.0 起子块正文不再随契约返回，拼接项恒等地不改变
		// bigram 覆盖（ranksOfChildContentIdentity 有表驱动证明）。
		newlyCovered := false
		for index, gold := range golds {
			if !covered[index] && overlapRatio(item.Content, gold) >= threshold {
				covered[index] = true
				newlyCovered = true
			}
		}
		if newlyCovered {
			ranks = append(ranks, position+1)
		}
	}
	return ranks
}

func thresholdList(cfg evalConfig) []float64 {
	seen := map[float64]struct{}{}
	list := []float64{}
	for _, threshold := range append([]float64{cfg.Overlap.Threshold}, cfg.Overlap.SensitivityThresholds...) {
		if threshold <= 0 || threshold > 1 {
			continue
		}
		if _, ok := seen[threshold]; ok {
			continue
		}
		seen[threshold] = struct{}{}
		list = append(list, threshold)
	}
	sort.Float64s(list)
	return list
}

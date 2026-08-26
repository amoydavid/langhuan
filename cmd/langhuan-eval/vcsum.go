package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// VCSUM 数据集（https://github.com/hahahawu/VCSum，MIT）：
// 239 场真实中文会议转写，人工标注话题切分（eos_index，闭区间结束下标）。
// 用途：无结构连续文本（会议转写/ASR 口语文本）的分块 + 检索全链路评测。
//
//	prepare 产物沿用通用布局（dataset.go）：
//	  track-a-corpus.jsonl  每个人工话题段一份短文档（隔离分块变量）
//	  track-b-corpus.jsonl  每场完整会议一份长文档（utterance 为段落）
//	  queries.jsonl         人工撰写的 query（vcsum_queries.json，一段一问）
//	  qrels.jsonl           query -> 话题段 docid（vcsum-m<会议>#<段号>）
//
// 数据完整性前提：只有「eos_index 切分与 short_* 段记录逐 utterance 完全
// 一致」的会议进入语料；不一致的会议整场剔除，不做修补。
const (
	vcsumSourceBase          = "https://raw.githubusercontent.com/hahahawu/VCSum/main/vcsum_data"
	vcsumDatasetName         = "vcsum"
	vcsumSeed          int64 = 20260826
	vcsumQueryMeetings       = 30
)

// vcsum 语料变体（oracle 实验，RETRIEVAL_BENCHMARK.md §6）：
//
//	""               原文转写（基线）
//	heading          每个话题段首注入人工话题标题（边界对齐 + 标题进上下文头）
//	heading-neutral  注入无信息量标题（仅隔离「边界对齐」单一变量）
//	heading-llm      注入 LLM 从段正文生成的标题（可实现版对照：能吃到人工上限的几成）
const (
	vcsumVariantPlain          = ""
	vcsumVariantHeading        = "heading"
	vcsumVariantHeadingNeutral = "heading-neutral"
	vcsumVariantHeadingLLM     = "heading-llm"
)

//go:embed vcsum_queries.json
var vcsumQueryAsset []byte

type vcsumPrepareOptions struct {
	DataDir       string
	CacheDir      string
	SourceBaseURL string
	QueryMeetings int
	Variant       string
	LLM           vcsumLLMOptions
}

// vcsumLLMOptions 是 heading-llm 变体的标题生成端点（OpenAI-compatible chat）。
type vcsumLLMOptions struct {
	BaseURL     string
	Model       string
	APIKeyFile  string
	Concurrency int
}

// vcsumDatasetDirName 返回变体对应的数据集目录与 manifest 名称。
func vcsumDatasetDirName(variant string) string {
	if variant == vcsumVariantPlain {
		return vcsumDatasetName
	}
	return vcsumDatasetName + "-" + variant
}

// vcsumMeeting 是 overall_context.txt 的一行。
type vcsumMeeting struct {
	ID       string     `json:"id"`
	EOSIndex []int      `json:"eos_index"`
	Context  [][]string `json:"context"` // 每个元素是一场内一个 utterance（句子列表）
}

// vcsumSegment 是 short_{train,dev,test}.txt 的一行（id 形如 "13_0"）。
type vcsumSegment struct {
	ID         string     `json:"id"`
	Context    [][]string `json:"context"`
	Agenda     string     `json:"agenda"`
	Discussion string     `json:"discussion"`
}

type vcsumQueryAssetItem struct {
	Meeting string `json:"meeting"`
	Seg     int    `json:"seg"`
	Query   string `json:"query"`
}

func prepareVCSUM(options vcsumPrepareOptions) error {
	if options.QueryMeetings <= 0 {
		options.QueryMeetings = vcsumQueryMeetings
	}
	switch options.Variant {
	case vcsumVariantPlain, vcsumVariantHeading, vcsumVariantHeadingNeutral, vcsumVariantHeadingLLM:
	default:
		return fmt.Errorf("未知 vcsum 变体 %q（可用：空 / heading / heading-neutral / heading-llm）", options.Variant)
	}
	datasetDir := filepath.Join(options.DataDir, vcsumDatasetDirName(options.Variant))
	if err := os.MkdirAll(datasetDir, 0o755); err != nil {
		return err
	}

	files := []string{"overall_context.txt", "short_train.txt", "short_dev.txt", "short_test.txt"}
	sourceSums := make(map[string]string, len(files))
	segments := make(map[string]map[int]vcsumSegment) // meetingID -> segIndex -> record
	var meetings []vcsumMeeting
	for _, name := range files {
		fmt.Printf("[vcsum] 获取 %s…\n", name)
		path, sum, err := downloadVCSumFile(options.CacheDir, options.SourceBaseURL, name)
		if err != nil {
			return err
		}
		sourceSums[name] = sum
		if name == "overall_context.txt" {
			if meetings, err = parseVCSumMeetings(path); err != nil {
				return err
			}
			continue
		}
		splitSegments, err := parseVCSumSegments(path)
		if err != nil {
			return err
		}
		for _, item := range splitSegments {
			if segments[item.meetingID] == nil {
				segments[item.meetingID] = make(map[int]vcsumSegment)
			}
			segments[item.meetingID][item.segIndex] = item.record
		}
	}

	aligned := make([]vcsumMeeting, 0, len(meetings))
	for _, meeting := range meetings {
		if vcsumMeetingAligned(meeting, segments) {
			aligned = append(aligned, meeting)
		}
	}
	fmt.Printf("[vcsum] 对齐完整会议 %d/%d（其余整场剔除）\n", len(aligned), len(meetings))
	if len(aligned) < options.QueryMeetings {
		return fmt.Errorf("对齐会议不足 %d 场，无法构建 query 集", options.QueryMeetings)
	}

	var queries []evalQuery
	var qrels []evalQrel
	var trackA []trackADoc
	var trackB []trackBDoc
	headingTitles, err := vcsumBuildHeadingTitles(options, aligned, segments)
	if err != nil {
		return err
	}
	queryMeetings := make(map[string]struct{}, options.QueryMeetings)
	for index, meeting := range aligned {
		utterances := vcsumUtterances(meeting.Context)
		trackB = append(trackB, trackBDoc{
			DocID:    vcsumMeetingDocID(meeting.ID),
			Title:    "会议" + meeting.ID + "转写",
			Passages: vcsumTrackBPassages(options.Variant, meeting, utterances, headingTitles[meeting.ID]),
		})
		if index < options.QueryMeetings {
			queryMeetings[meeting.ID] = struct{}{}
		}
		for segIndex, end := range meeting.EOSIndex {
			record := segments[meeting.ID][segIndex]
			segUtterances := vcsumUtterances(meeting.Context[vcsumSegmentStart(meeting, segIndex) : end+1])
			title := strings.TrimSpace(record.Agenda)
			if title == "" {
				title = fmt.Sprintf("会议%s话题段%d", meeting.ID, segIndex)
			}
			trackA = append(trackA, trackADoc{
				DocID: vcsumSegmentDocID(meeting.ID, segIndex),
				Title: title, Text: strings.Join(segUtterances, "\n\n"),
			})
		}
	}

	var queryAsset []vcsumQueryAssetItem
	if err := json.Unmarshal(vcsumQueryAsset, &queryAsset); err != nil {
		return fmt.Errorf("解析内嵌 query 资产失败: %w", err)
	}
	segmentDocIDs := make(map[string]struct{}, len(trackA))
	for _, doc := range trackA {
		segmentDocIDs[doc.DocID] = struct{}{}
	}
	for _, item := range queryAsset {
		if _, ok := queryMeetings[item.Meeting]; !ok {
			return fmt.Errorf("query 资产引用了非 query 会议 %s（资产与 --query-meetings 不匹配）", item.Meeting)
		}
		docID := vcsumSegmentDocID(item.Meeting, item.Seg)
		if _, ok := segmentDocIDs[docID]; !ok {
			return fmt.Errorf("query 资产引用了不存在的话题段 %s", docID)
		}
		queryID := fmt.Sprintf("q%s_%d", item.Meeting, item.Seg)
		queries = append(queries, evalQuery{QueryID: queryID, Query: item.Query})
		qrels = append(qrels, evalQrel{QueryID: queryID, DocID: docID, Relevance: 1})
	}

	m := manifest{
		Dataset: vcsumDatasetDirName(options.Variant), Seed: vcsumSeed,
		QueryCount: len(queries), TrackACorpusSize: len(trackA), TrackBCorpusSize: len(trackB),
		SourceFiles: sourceSums,
		GeneratedBy: "langhuan-eval vcsum（query 集为仓库内人工撰写资产 vcsum_queries.json，一段一问）",
	}
	if options.Variant != vcsumVariantPlain {
		m.GeneratedBy += "；变体 " + options.Variant + "（话题段首注入标题，oracle 实验）"
	}
	if options.Variant == vcsumVariantHeadingLLM {
		m.GeneratedBy += "；标题由 " + options.LLM.Model + " 生成（温度 0，缓存于 cache/vcsum-llm-titles）"
	}
	if err := writeJSONL(filepath.Join(datasetDir, "queries.jsonl"), queries); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(datasetDir, "qrels.jsonl"), qrels); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(datasetDir, "track-a-corpus.jsonl"), trackA); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(datasetDir, "track-b-corpus.jsonl"), trackB); err != nil {
		return err
	}
	manifestBody, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(datasetDir, "manifest.json"), manifestBody, 0o644); err != nil {
		return err
	}
	fmt.Printf("[vcsum] 数据集就绪：%s（query=%d TrackA=%d TrackB=%d）\n",
		datasetDir, len(queries), len(trackA), len(trackB))
	return nil
}

func vcsumMeetingDocID(meetingID string) string {
	return "vcsum-m" + meetingID
}

func vcsumSegmentDocID(meetingID string, segIndex int) string {
	return fmt.Sprintf("vcsum-m%s#%d", meetingID, segIndex)
}

// vcsumSegmentStart 返回第 segIndex 段的首个 utterance 下标（eos_index 为
// 每段闭区间结束下标）。
func vcsumSegmentStart(meeting vcsumMeeting, segIndex int) int {
	start := 0
	for index := 0; index < segIndex; index++ {
		start = meeting.EOSIndex[index] + 1
	}
	return start
}

// vcsumUtterances 把逐 utterance 的句子列表转为段落文本（utterance 内多行拼接，
// utterance 之间由调用方用空行连接，保证 track-a 段文本是 track-b 文档的子串）。
func vcsumUtterances(context [][]string) []string {
	utterances := make([]string, len(context))
	for index, sentences := range context {
		utterances[index] = strings.Join(sentences, "\n")
	}
	return utterances
}

// vcsumTrackBPassages 组装 track-b 长文档段落：heading 变体在每个话题段首
// 注入标题 passage（markdown 解析器识别为 heading 块 → chunker 沿话题边界
// 切分并把标题写入各块 HeadingPath/ContextHeader）。
func vcsumTrackBPassages(variant string, meeting vcsumMeeting, utterances []string, titles map[int]string) []string {
	if variant == vcsumVariantPlain {
		return utterances
	}
	passages := make([]string, 0, len(utterances)+len(meeting.EOSIndex))
	for segIndex, end := range meeting.EOSIndex {
		title := strings.TrimSpace(titles[segIndex])
		if title == "" {
			title = fmt.Sprintf("话题段%d", segIndex)
		}
		passages = append(passages, "## "+title)
		passages = append(passages, utterances[vcsumSegmentStart(meeting, segIndex):end+1]...)
	}
	return passages
}

// vcsumBuildHeadingTitles 返回 meetingID -> segIndex -> 标题文本（不含 ## 前缀）。
// plain 变体返回空 map；heading 用人工 agenda；heading-neutral 用固定中性标题；
// heading-llm 调 LLM 从段正文生成（带缓存，失败段回退中性标题）。
func vcsumBuildHeadingTitles(
	options vcsumPrepareOptions,
	aligned []vcsumMeeting,
	segments map[string]map[int]vcsumSegment,
) (map[string]map[int]string, error) {
	result := make(map[string]map[int]string, len(aligned))
	if options.Variant == vcsumVariantPlain {
		return result, nil
	}
	for _, meeting := range aligned {
		meetingTitles := make(map[int]string, len(meeting.EOSIndex))
		for segIndex := range meeting.EOSIndex {
			switch options.Variant {
			case vcsumVariantHeading:
				meetingTitles[segIndex] = strings.TrimSpace(segments[meeting.ID][segIndex].Agenda)
			case vcsumVariantHeadingNeutral:
				meetingTitles[segIndex] = fmt.Sprintf("话题段%d", segIndex)
			case vcsumVariantHeadingLLM:
				// 由 generateVCSumLLMTitles 填充。
			}
		}
		result[meeting.ID] = meetingTitles
	}
	if options.Variant != vcsumVariantHeadingLLM {
		return result, nil
	}
	generated, err := generateVCSumLLMTitles(options, aligned, segments)
	if err != nil {
		return nil, err
	}
	fallbacks := 0
	for _, meeting := range aligned {
		for segIndex := range meeting.EOSIndex {
			title := strings.TrimSpace(generated[vcsumSegmentDocID(meeting.ID, segIndex)])
			if title == "" {
				title = fmt.Sprintf("话题段%d", segIndex)
				fallbacks++
			}
			result[meeting.ID][segIndex] = title
		}
	}
	if fallbacks > 0 {
		fmt.Printf("[vcsum] LLM 标题生成失败回退中性标题的段数：%d\n", fallbacks)
	}
	return result, nil
}

// vcsumLLMTitleCache 是缓存文件结构：模型或 prompt 版本变化时整体失效重生成。
type vcsumLLMTitleCache struct {
	Model         string            `json:"model"`
	PromptVersion string            `json:"prompt_version"`
	Titles        map[string]string `json:"titles"` // 段 docid -> 标题
}

const vcsumLLMTitlePromptVersion = "v1"

// generateVCSumLLMTitles 为全部对齐会议的话题段生成标题，缓存于
// <cacheDir>/vcsum-llm-titles/<model>.json。并发调用 LLM，逐批落盘，
// 中断重跑只补缺失段。
func generateVCSumLLMTitles(
	options vcsumPrepareOptions,
	aligned []vcsumMeeting,
	segments map[string]map[int]vcsumSegment,
) (map[string]string, error) {
	if options.LLM.BaseURL == "" || options.LLM.Model == "" {
		return nil, fmt.Errorf("heading-llm 变体需要 -llm-base-url 与 -llm-model")
	}
	cachePath := filepath.Join(options.CacheDir, "vcsum-llm-titles", sanitizeName(options.LLM.Model)+".json")
	cache := vcsumLLMTitleCache{Model: options.LLM.Model, PromptVersion: vcsumLLMTitlePromptVersion, Titles: map[string]string{}}
	if body, err := os.ReadFile(cachePath); err == nil {
		var loaded vcsumLLMTitleCache
		if err := json.Unmarshal(body, &loaded); err == nil &&
			loaded.Model == cache.Model && loaded.PromptVersion == cache.PromptVersion {
			cache.Titles = loaded.Titles
		}
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, err
	}
	apiKey := ""
	if options.LLM.APIKeyFile != "" {
		body, err := os.ReadFile(options.LLM.APIKeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取 llm api key file 失败: %w", err)
		}
		apiKey = strings.TrimSpace(string(body))
	}

	type segJob struct {
		docID string
		text  string
	}
	jobs := make([]segJob, 0)
	for _, meeting := range aligned {
		for segIndex, end := range meeting.EOSIndex {
			docID := vcsumSegmentDocID(meeting.ID, segIndex)
			if strings.TrimSpace(cache.Titles[docID]) != "" {
				continue
			}
			utterances := vcsumUtterances(meeting.Context[vcsumSegmentStart(meeting, segIndex) : end+1])
			jobs = append(jobs, segJob{docID: docID, text: strings.Join(utterances, "\n\n")})
		}
	}
	fmt.Printf("[vcsum] LLM 标题生成：%s（%s），待生成 %d 段（缓存命中 %d）\n",
		options.LLM.Model, options.LLM.BaseURL, len(jobs), len(cache.Titles))
	if len(jobs) == 0 {
		return cache.Titles, nil
	}

	workers := options.LLM.Concurrency
	if workers < 1 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	failed := 0
	for _, job := range jobs {
		wg.Add(1)
		go func(job segJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			title, err := vcsumCallLLMForTitle(options.LLM, apiKey, job.text)
			if err != nil {
				fmt.Printf("  [warn] %s 生成失败: %v\n", job.docID, err)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			cache.Titles[job.docID] = title
			mu.Unlock()
			if len(cache.Titles)%100 == 0 {
				mu.Lock()
				body, err := json.MarshalIndent(cache, "", " ")
				mu.Unlock()
				if err == nil {
					_ = os.WriteFile(cachePath, body, 0o644)
				}
				fmt.Printf("  已生成 %d 段\n", len(cache.Titles))
			}
		}(job)
	}
	wg.Wait()
	if failed > 0 {
		fmt.Printf("[vcsum] 生成失败段数 %d（这些段将回退中性标题，可重跑 prepare 补齐）\n", failed)
	}
	body, err := json.MarshalIndent(cache, "", " ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cachePath, body, 0o644); err != nil {
		return nil, err
	}
	return cache.Titles, nil
}

// vcsumCallLLMForTitle 调 OpenAI-compatible chat 接口生成一个段标题。
func vcsumCallLLMForTitle(options vcsumLLMOptions, apiKey, text string) (string, error) {
	prompt := "给下面这段中文会议转写起一个简短的话题标题（6~15个字，概括讨论主题，" +
		"像目录条目一样）。只输出标题本身，不要任何解释、引号或标点结尾。\n\n" + text
	body, err := json.Marshal(map[string]any{
		"model": options.Model, "temperature": 0,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(options.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.Do(request)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
		response.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", response.StatusCode, truncateForLog(string(raw), 200))
			time.Sleep(time.Second)
			continue
		}
		var decoded struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Choices) == 0 {
			lastErr = fmt.Errorf("响应缺少 choices")
			continue
		}
		title := cleanVCSumLLMTitle(decoded.Choices[0].Message.Content)
		if title == "" {
			lastErr = fmt.Errorf("生成标题为空")
			continue
		}
		return title, nil
	}
	return "", lastErr
}

// cleanVCSumLLMTitle 清洗模型输出：取首行、去引号/空白/句末标点，限长 30 字。
func cleanVCSumLLMTitle(raw string) string {
	firstLine, _, _ := strings.Cut(raw, "\n")
	firstLine = strings.TrimSpace(firstLine)
	for {
		trimmed := strings.Trim(firstLine, "\"'“”‘’《》「」 \t")
		trimmed = strings.TrimRight(trimmed, "。.!！?？：:；;，, ")
		if trimmed == firstLine {
			break
		}
		firstLine = trimmed
	}
	if count := utf8.RuneCountInString(firstLine); count > 30 {
		firstLine = string([]rune(firstLine)[:30])
	}
	return firstLine
}

func parseVCSumMeetings(path string) ([]vcsumMeeting, error) {
	rows, err := readJSONL[vcsumMeeting](path)
	if err != nil {
		return nil, fmt.Errorf("解析 overall_context.txt 失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("overall_context.txt 为空")
	}
	return rows, nil
}

type vcsumSplitSegment struct {
	meetingID string
	segIndex  int
	record    vcsumSegment
}

func parseVCSumSegments(path string) ([]vcsumSplitSegment, error) {
	rows, err := readJSONL[vcsumSegment](path)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", filepath.Base(path), err)
	}
	result := make([]vcsumSplitSegment, 0, len(rows))
	for _, row := range rows {
		meetingID, segIndex, ok := strings.Cut(row.ID, "_")
		if !ok {
			return nil, fmt.Errorf("段 id %q 缺少下划线分隔", row.ID)
		}
		index := 0
		if _, err := fmt.Sscanf(segIndex, "%d", &index); err != nil {
			return nil, fmt.Errorf("段 id %q 的段号不合法: %w", row.ID, err)
		}
		result = append(result, vcsumSplitSegment{meetingID: meetingID, segIndex: index, record: row})
	}
	return result, nil
}

// vcsumMeetingAligned 校验一场会议的 eos_index 切分与 short_* 段记录
// 逐 utterance 完全一致。
func vcsumMeetingAligned(meeting vcsumMeeting, segments map[string]map[int]vcsumSegment) bool {
	if len(meeting.EOSIndex) == 0 || meeting.EOSIndex[len(meeting.EOSIndex)-1] != len(meeting.Context)-1 {
		return false
	}
	records := segments[meeting.ID]
	if len(records) != len(meeting.EOSIndex) {
		return false
	}
	prev := 0
	for segIndex, end := range meeting.EOSIndex {
		record, ok := records[segIndex]
		if !ok || !vcsumContextEqual(record.Context, meeting.Context[prev:end+1]) {
			return false
		}
		prev = end + 1
	}
	return true
}

func vcsumContextEqual(left, right [][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if len(left[index]) != len(right[index]) {
			return false
		}
		for sentence := range left[index] {
			if left[index][sentence] != right[index][sentence] {
				return false
			}
		}
	}
	return true
}

// downloadVCSumFile 从源仓库下载单个文件到缓存（存在即复用），返回路径与 sha256。
func downloadVCSumFile(cacheDir, baseURL, name string) (string, string, error) {
	if baseURL == "" {
		baseURL = vcsumSourceBase
	}
	localPath := filepath.Join(cacheDir, vcsumDatasetName, name)
	if sum, ok := cachedSHA256(localPath); ok {
		return localPath, sum, nil
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", "", err
	}
	url := strings.TrimRight(baseURL, "/") + "/" + name
	client := &http.Client{Timeout: 30 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("下载 %s 失败: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下载 %s 失败: HTTP %d", name, response.StatusCode)
	}
	tmp := localPath + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return "", "", err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return "", "", copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", "", closeErr
	}
	if err := os.Rename(tmp, localPath); err != nil {
		return "", "", err
	}
	sum, err := fileSHA256(localPath)
	if err != nil {
		return "", "", err
	}
	return localPath, sum, nil
}

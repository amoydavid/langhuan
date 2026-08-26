package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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
const (
	vcsumVariantPlain          = ""
	vcsumVariantHeading        = "heading"
	vcsumVariantHeadingNeutral = "heading-neutral"
)

//go:embed vcsum_queries.json
var vcsumQueryAsset []byte

type vcsumPrepareOptions struct {
	DataDir       string
	CacheDir      string
	SourceBaseURL string
	QueryMeetings int
	Variant       string
}

// vcsumDatasetDirName 返回变体对应的数据集目录与 manifest 名称。
func vcsumDatasetDirName(variant string) string {
	if variant == vcsumVariantPlain {
		return vcsumDatasetName
	}
	return vcsumDatasetName + "-" + variant
}

// vcsumHeadingPassage 返回变体在段首注入的标题 passage；非 heading 变体不注入。
func vcsumHeadingPassage(variant string, record vcsumSegment, segIndex int) string {
	switch variant {
	case vcsumVariantHeading:
		title := strings.TrimSpace(record.Agenda)
		if title == "" {
			title = fmt.Sprintf("话题段%d", segIndex)
		}
		return "## " + title
	case vcsumVariantHeadingNeutral:
		return fmt.Sprintf("## 话题段%d", segIndex)
	}
	return ""
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
	case vcsumVariantPlain, vcsumVariantHeading, vcsumVariantHeadingNeutral:
	default:
		return fmt.Errorf("未知 vcsum 变体 %q（可用：空 / heading / heading-neutral）", options.Variant)
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
	queryMeetings := make(map[string]struct{}, options.QueryMeetings)
	for index, meeting := range aligned {
		utterances := vcsumUtterances(meeting.Context)
		trackB = append(trackB, trackBDoc{
			DocID:    vcsumMeetingDocID(meeting.ID),
			Title:    "会议" + meeting.ID + "转写",
			Passages: vcsumTrackBPassages(options.Variant, meeting, utterances, segments[meeting.ID]),
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
func vcsumTrackBPassages(variant string, meeting vcsumMeeting, utterances []string, records map[int]vcsumSegment) []string {
	if variant == vcsumVariantPlain {
		return utterances
	}
	passages := make([]string, 0, len(utterances)+len(meeting.EOSIndex))
	for segIndex, end := range meeting.EOSIndex {
		if heading := vcsumHeadingPassage(variant, records[segIndex], segIndex); heading != "" {
			passages = append(passages, heading)
		}
		passages = append(passages, utterances[vcsumSegmentStart(meeting, segIndex):end+1]...)
	}
	return passages
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

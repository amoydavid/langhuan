package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// 本地评测数据集布局（spec §5.3）：
//
//	<dir>/manifest.json           采样指纹（seed、来源文件 sha256、计数）
//	<dir>/queries.jsonl           {query_id, query}
//	<dir>/qrels.jsonl             {query_id, docid, relevance}（段落级 gold）
//	<dir>/track-a-corpus.jsonl    {docid, title, text}（一段落一文档）
//	<dir>/track-b-corpus.jsonl    {docid, title, passages[]}（按 Wikipedia 文章聚合）
type evalQuery struct {
	QueryID string `json:"query_id"`
	Query   string `json:"query"`
}

type evalQrel struct {
	QueryID   string `json:"query_id"`
	DocID     string `json:"docid"`
	Relevance int    `json:"relevance"`
}

type trackADoc struct {
	DocID string `json:"docid"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type trackBDoc struct {
	DocID    string   `json:"docid"` // 文章 ID（docid '#' 前缀）
	Title    string   `json:"title"`
	Passages []string `json:"passages"`
}

type manifest struct {
	Dataset               string            `json:"dataset"`
	Seed                  int64             `json:"seed"`
	QueryCount            int               `json:"query_count"`
	TrackACorpusSize      int               `json:"track_a_corpus_size"`
	TrackBCorpusSize      int               `json:"track_b_corpus_size"`
	Distractors           int               `json:"track_a_distractors"`
	DistractorArticles    int               `json:"track_b_distractor_articles"`
	MaxPassagesPerArticle int               `json:"max_passages_per_article"`
	SourceFiles           map[string]string `json:"source_files"` // 相对路径 -> sha256
	GeneratedBy           string            `json:"generated_by"`
}

// evalDataset 是 prepare 的产物、run 的输入。
type evalDataset struct {
	Manifest manifest
	Queries  []evalQuery
	Qrels    []evalQrel
	TrackA   []trackADoc
	TrackB   []trackBDoc
	Dir      string
}

// goldPassagesByQuery 返回 query_id -> gold 段落文本集合（relevance ≥ 1）。
// 两个轨道共享同一 gold 定义：Track A 用检索结果正文与 gold 文本重叠判定，
// Track B 用父块正文与同一 gold 文本重叠判定。
func (d *evalDataset) goldPassagesByQuery() map[string][]string {
	goldDocIDs := make(map[string]map[string]struct{})
	for _, qrel := range d.Qrels {
		if qrel.Relevance < 1 {
			continue
		}
		if goldDocIDs[qrel.QueryID] == nil {
			goldDocIDs[qrel.QueryID] = map[string]struct{}{}
		}
		goldDocIDs[qrel.QueryID][qrel.DocID] = struct{}{}
	}
	textByID := make(map[string]string, len(d.TrackA))
	for _, doc := range d.TrackA {
		textByID[doc.DocID] = doc.Text
	}
	golds := make(map[string][]string, len(goldDocIDs))
	for queryID, ids := range goldDocIDs {
		for id := range ids {
			if text, ok := textByID[id]; ok {
				golds[queryID] = append(golds[queryID], text)
			}
		}
	}
	return golds
}

func writeJSONL[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func readJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []T
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row T
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
		result = append(result, row)
	}
	return result, scanner.Err()
}

func loadDataset(dir string) (*evalDataset, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("数据集不存在：%s（先运行 langhuan-eval prepare）", dir)
	}
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()
	var m manifest
	if err := json.NewDecoder(manifestFile).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析 manifest 失败: %w", err)
	}
	queries, err := readJSONL[evalQuery](filepath.Join(dir, "queries.jsonl"))
	if err != nil {
		return nil, err
	}
	qrels, err := readJSONL[evalQrel](filepath.Join(dir, "qrels.jsonl"))
	if err != nil {
		return nil, err
	}
	trackA, err := readJSONL[trackADoc](filepath.Join(dir, "track-a-corpus.jsonl"))
	if err != nil {
		return nil, err
	}
	trackB, err := readJSONL[trackBDoc](filepath.Join(dir, "track-b-corpus.jsonl"))
	if err != nil {
		return nil, err
	}
	return &evalDataset{Manifest: m, Queries: queries, Qrels: qrels, TrackA: trackA, TrackB: trackB, Dir: dir}, nil
}

// goldDocTokensByQuery 返回 query_id -> gold 文档标记集合：Track A 是段落
// docid 本身，Track B 取 docid 的文章前缀（longDoc=true）。标记与导入标题的
// "[docid]" 后缀对应，用于未命中归因（gold 文档是否被召回）。
func (d *evalDataset) goldDocTokensByQuery(longDoc bool) map[string][]string {
	tokens := make(map[string][]string)
	for _, qrel := range d.Qrels {
		if qrel.Relevance < 1 {
			continue
		}
		id := qrel.DocID
		if longDoc {
			id = articleOf(id)
		}
		seen := false
		for _, existing := range tokens[qrel.QueryID] {
			if existing == id {
				seen = true
				break
			}
		}
		if !seen {
			tokens[qrel.QueryID] = append(tokens[qrel.QueryID], id)
		}
	}
	return tokens
}

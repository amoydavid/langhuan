package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// prepare 流程（spec §5）：下载 MIRACL-zh 原始文件 → 确定性子采样 →
// 落地双轨数据集 + manifest。所有随机性都来自固定 seed 与 FNV 哈希，
// 同版本原始文件重复 prepare 产出逐字节一致。
type prepareOptions struct {
	DataDir               string
	CacheDir              string
	Mirror                string
	Fallback              string
	Queries               int
	Distractors           int
	DistractorArticles    int
	MaxPassagesPerArticle int
	Seed                  int64
}

const (
	miraclRepoCorpus = "miracl/miracl-corpus"
	miraclRepoMeta   = "miracl/miracl"
	miraclShardCount = 10
	// 语料规模常数用于哈希过滤分母（zh：~493 万段落 / ~125 万文章）。
	zhPassageApprox = 4_934_368
	zhArticleApprox = 1_246_389
)

type miraclPassage struct {
	DocID string `json:"docid"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func prepareMIRACLChinese(options prepareOptions) error {
	hf := newHFClient(options.Mirror, options.Fallback, options.CacheDir)
	datasetDir := filepath.Join(options.DataDir, "miracl-zh")
	if err := os.MkdirAll(datasetDir, 0o755); err != nil {
		return err
	}

	fmt.Println("[1/4] 下载 topics/qrels（MIRACL-zh dev）…")
	topicsPath, topicsSum, err := hf.downloadDatasetFile(
		miraclRepoMeta, "miracl-v1.0-zh/topics/topics.miracl-v1.0-zh-dev.tsv")
	if err != nil {
		return err
	}
	qrelsPath, qrelsSum, err := hf.downloadDatasetFile(
		miraclRepoMeta, "miracl-v1.0-zh/qrels/qrels.miracl-v1.0-zh-dev.tsv")
	if err != nil {
		return err
	}

	topics, err := parseMIRACLTopics(topicsPath)
	if err != nil {
		return err
	}
	allQrels, err := parseMIRACLQrels(qrelsPath)
	if err != nil {
		return err
	}
	goldDocIDsByQuery := make(map[string]map[string]struct{})
	for _, qrel := range allQrels {
		if qrel.Relevance < 1 {
			continue
		}
		if goldDocIDsByQuery[qrel.QueryID] == nil {
			goldDocIDsByQuery[qrel.QueryID] = map[string]struct{}{}
		}
		goldDocIDsByQuery[qrel.QueryID][qrel.DocID] = struct{}{}
	}

	// 确定性采样：按 qid 排序后用固定 seed 洗牌，取前 N，再按 qid 稳序输出。
	qids := make([]string, 0, len(goldDocIDsByQuery))
	for qid := range goldDocIDsByQuery {
		if _, ok := topics[qid]; ok {
			qids = append(qids, qid)
		}
	}
	sort.Strings(qids)
	random := rand.New(rand.NewSource(options.Seed))
	random.Shuffle(len(qids), func(i, j int) { qids[i], qids[j] = qids[j], qids[i] })
	if len(qids) > options.Queries {
		qids = qids[:options.Queries]
	}
	sort.Strings(qids)
	if len(qids) == 0 {
		return fmt.Errorf("没有可用 query（topics/qrels 不匹配？）")
	}

	selected := make(map[string]struct{}, len(qids))
	for _, qid := range qids {
		selected[qid] = struct{}{}
	}
	goldDocIDs := map[string]struct{}{}
	goldArticles := map[string]struct{}{}
	for qid, ids := range goldDocIDsByQuery {
		if _, ok := selected[qid]; !ok {
			continue
		}
		for id := range ids {
			goldDocIDs[id] = struct{}{}
			goldArticles[articleOf(id)] = struct{}{}
		}
	}

	fmt.Printf("[2/4] 下载并扫描 %d 个语料分片（首次约 730MB，已缓存则秒过）…\n", miraclShardCount)
	// 干扰项预过滤池：Track A 段落池约 4 倍目标，Track B 文章池约 4 倍目标。
	passagePoolMod := zhPassageApprox / (options.Distractors * 4)
	articlePoolMod := zhArticleApprox / (options.DistractorArticles * 4)
	if passagePoolMod < 1 {
		passagePoolMod = 1
	}
	if articlePoolMod < 1 {
		articlePoolMod = 1
	}
	scan := &miraclScanState{
		goldDocIDs: goldDocIDs, goldArticles: goldArticles,
		goldPassages:    make(map[string]miraclPassage, len(goldDocIDs)),
		articlePassages: make(map[string][]miraclPassage, len(goldArticles)),
		articlePool:     make(map[string][]miraclPassage),
		passagePoolMod:  passagePoolMod, articlePoolMod: articlePoolMod,
	}
	sourceSums := map[string]string{
		"topics.tsv": topicsSum,
		"qrels.tsv":  qrelsSum,
	}
	for index := 0; index < miraclShardCount; index++ {
		remote := fmt.Sprintf("miracl-corpus-v1.0-zh/docs-%d.jsonl.gz", index)
		fmt.Printf("  分片 %d/%d：%s\n", index+1, miraclShardCount, remote)
		path, sum, err := hf.downloadDatasetFile(miraclRepoCorpus, remote)
		if err != nil {
			return err
		}
		sourceSums[filepath.Base(remote)] = sum
		if err := scan.scanShard(path); err != nil {
			return err
		}
	}

	// 扫描完成后：为 Track B 补齐 gold 文章的全部段落（goldArticles 全量收录）。
	fmt.Println("[3/4] 构建双轨语料…")
	trackA := buildTrackA(scan.goldPassages, goldDocIDs, scan.passagePool, options.Distractors)
	trackB, droppedArticles := buildTrackB(scan.articlePassages, goldArticles, scan.articlePool,
		options.DistractorArticles, options.MaxPassagesPerArticle)

	// 落地查询与 qrels；gold 段落全部缺失的 query 丢弃并记录。
	queries := make([]evalQuery, 0, len(qids))
	qrels := make([]evalQrel, 0, len(qids)*2)
	droppedQueries := 0
	for _, qid := range qids {
		found := false
		for id := range goldDocIDsByQuery[qid] {
			if _, ok := scan.goldPassages[id]; ok {
				found = true
				qrels = append(qrels, evalQrel{QueryID: qid, DocID: id, Relevance: 1})
			}
		}
		if !found {
			droppedQueries++
			continue
		}
		queries = append(queries, evalQuery{QueryID: qid, Query: topics[qid]})
	}

	fmt.Printf("[4/4] 写入数据集：%s（query=%d 段落语料=%d 长文档=%d 丢弃query=%d 丢弃文章=%d）\n",
		datasetDir, len(queries), len(trackA), len(trackB), droppedQueries, droppedArticles)
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
	m := manifest{
		Dataset: "miracl-zh", Seed: options.Seed, QueryCount: len(queries),
		TrackACorpusSize: len(trackA), TrackBCorpusSize: len(trackB),
		Distractors: options.Distractors, DistractorArticles: options.DistractorArticles,
		MaxPassagesPerArticle: options.MaxPassagesPerArticle,
		SourceFiles:           sourceSums, GeneratedBy: "langhuan-eval prepare (miracl-zh dev)",
	}
	manifestFile, err := os.Create(filepath.Join(datasetDir, "manifest.json"))
	if err != nil {
		return err
	}
	defer manifestFile.Close()
	encoder := json.NewEncoder(manifestFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(m)
}

// miraclScanState 承载全量语料扫描的累积状态；干扰项池用 FNV 哈希过滤，
// 采集结果与分片内行顺序无关，保证确定性。
type miraclScanState struct {
	goldDocIDs      map[string]struct{}
	goldArticles    map[string]struct{}
	goldPassages    map[string]miraclPassage
	articlePassages map[string][]miraclPassage
	passagePool     []miraclPassage
	articlePool     map[string][]miraclPassage
	passagePoolMod  int
	articlePoolMod  int
}

// scanShard 单遍扫描一个语料分片，同时收集 gold 段落、gold 文章段落
// 与两个确定性干扰项池。
func (s *miraclScanState) scanShard(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("解压 %s 失败: %w", path, err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var passage miraclPassage
		if err := json.Unmarshal(raw, &passage); err != nil {
			return fmt.Errorf("%s 第 %d 行解析失败: %w", path, line, err)
		}
		if passage.DocID == "" || strings.TrimSpace(passage.Text) == "" {
			continue
		}
		article := articleOf(passage.DocID)
		if _, ok := s.goldDocIDs[passage.DocID]; ok {
			s.goldPassages[passage.DocID] = passage
		}
		if _, ok := s.goldArticles[article]; ok {
			s.articlePassages[article] = append(s.articlePassages[article], passage)
		}
		if _, ok := s.goldDocIDs[passage.DocID]; !ok {
			if fnvHash(passage.DocID)%uint64(s.passagePoolMod) == 0 {
				s.passagePool = append(s.passagePool, passage)
			}
			if fnvHash(article)%uint64(s.articlePoolMod) == 0 {
				s.articlePool[article] = append(s.articlePool[article], passage)
			}
		}
	}
	return scanner.Err()
}

// buildTrackA 组装段落轨道语料：gold 段落 + 确定性干扰段落（按 docid 的
// FNV 哈希排序取前 N），输出按 docid 稳序。
func buildTrackA(
	goldPassages map[string]miraclPassage,
	goldDocIDs map[string]struct{},
	pool []miraclPassage,
	distractors int,
) []trackADoc {
	type keyed struct {
		hash uint64
		doc  trackADoc
	}
	candidates := make([]keyed, 0, len(pool))
	for _, passage := range pool {
		if _, ok := goldDocIDs[passage.DocID]; ok {
			continue
		}
		candidates = append(candidates, keyed{fnvHash(passage.DocID), trackADoc{
			DocID: passage.DocID, Title: passage.Title, Text: passage.Text,
		}})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].hash != candidates[j].hash {
			return candidates[i].hash < candidates[j].hash
		}
		return candidates[i].doc.DocID < candidates[j].doc.DocID
	})
	if len(candidates) > distractors {
		candidates = candidates[:distractors]
	}
	result := make([]trackADoc, 0, len(candidates)+len(goldPassages))
	for _, candidate := range candidates {
		result = append(result, candidate.doc)
	}
	for id, passage := range goldPassages {
		result = append(result, trackADoc{DocID: id, Title: passage.Title, Text: passage.Text})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DocID < result[j].DocID })
	return result
}

// buildTrackB 组装长文档轨道语料：gold 文章（全段落）+ 确定性干扰文章
// （≥2 段落），段落按段落号排序并截断到上限。
func buildTrackB(
	articlePassages map[string][]miraclPassage,
	goldArticles map[string]struct{},
	pool map[string][]miraclPassage,
	distractorArticles, maxPassages int,
) ([]trackBDoc, int) {
	docs := make([]trackBDoc, 0, len(goldArticles)+distractorArticles)
	for article, passages := range articlePassages {
		if _, ok := goldArticles[article]; !ok {
			continue
		}
		docs = append(docs, articleToTrackB(article, passages, maxPassages))
	}
	goldCount := len(docs)

	type keyed struct {
		hash    uint64
		article string
	}
	candidates := make([]keyed, 0, len(pool))
	for article, passages := range pool {
		if _, ok := goldArticles[article]; ok {
			continue
		}
		if len(passages) < 2 {
			continue
		}
		candidates = append(candidates, keyed{fnvHash(article), article})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].hash != candidates[j].hash {
			return candidates[i].hash < candidates[j].hash
		}
		return candidates[i].article < candidates[j].article
	})
	dropped := 0
	for _, candidate := range candidates {
		if len(docs)-goldCount >= distractorArticles {
			break
		}
		docs = append(docs, articleToTrackB(candidate.article, pool[candidate.article], maxPassages))
	}
	if len(docs)-goldCount < distractorArticles {
		dropped = distractorArticles - (len(docs) - goldCount)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].DocID < docs[j].DocID })
	return docs, dropped
}

func articleToTrackB(article string, passages []miraclPassage, maxPassages int) trackBDoc {
	sorted := make([]miraclPassage, len(passages))
	copy(sorted, passages)
	sort.Slice(sorted, func(i, j int) bool {
		return passageSequence(sorted[i].DocID) < passageSequence(sorted[j].DocID)
	})
	if len(sorted) > maxPassages {
		sorted = sorted[:maxPassages]
	}
	title := sorted[0].Title
	texts := make([]string, len(sorted))
	for index, passage := range sorted {
		texts[index] = passage.Text
	}
	return trackBDoc{DocID: article, Title: title, Passages: texts}
}

// articleOf 取 docid 的文章前缀（'#' 之前）；无 '#' 时原样返回。
func articleOf(docID string) string {
	if index := strings.LastIndex(docID, "#"); index >= 0 {
		return docID[:index]
	}
	return docID
}

// passageSequence 取 docid 的段落号（'#' 之后）；解析失败按 0。
func passageSequence(docID string) int {
	if index := strings.LastIndex(docID, "#"); index >= 0 {
		if seq, err := strconv.Atoi(docID[index+1:]); err == nil {
			return seq
		}
	}
	return 0
}

func fnvHash(text string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(text))
	return hash.Sum64()
}

func parseMIRACLTopics(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	topics := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) >= 2 && fields[0] != "" {
			topics[fields[0]] = strings.TrimSpace(fields[1])
		}
	}
	return topics, scanner.Err()
}

func parseMIRACLQrels(path string) ([]evalQrel, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var qrels []evalQrel
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 4 {
			continue
		}
		relevance, err := strconv.Atoi(strings.TrimSpace(fields[3]))
		if err != nil {
			continue
		}
		qrels = append(qrels, evalQrel{QueryID: fields[0], DocID: fields[2], Relevance: relevance})
	}
	return qrels, scanner.Err()
}

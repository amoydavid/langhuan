# 离线检索评测基准设计（公开数据集 + 评测 harness）

> 状态：已实现（面向 v1.2.0；T1–T5 交付，操作指南见 §13、结果解读见 §14）  
> 范围：评测数据集准备、独立评测 harness、检索指标与报告、检索通道对比  
> 不包含：线上 A/B 实验、LLM 生成质量评测、任何人工标注、LLM-as-judge

## 1. 背景

琅嬛当前的测试体系（单元/集成/E2E）全部是正确性测试，回答"功能对不对"；没有任何量化评测回答"检索好不好"。由此产生的具体缺口：

- 分块合同从 v1 演进到 v3（单层 512/80 → 父子 4096/384），每次演进没有指标依据。
- RRF 混合检索（vector + FTS）与 Rerank 的实际贡献从未被度量，"混合优于单路"只是行业共识，不是本系统的实测结论。
- Embedding 模型在自有管线（分块 → search_content → 检索）中的表现只能靠人工观感判断。

两份既有规格已把该缺口列为待办：`2026-08-05-rerank-model-and-search-design.md`（"建立离线检索评测数据集，对比 RRF 与 Rerank 的 nDCG/MRR/Recall，而不是依赖单次人工观感"）与 `2026-08-10-search-evidence-lineage-replay-design.md`（"基于 qrels 的 Recall/MRR/nDCG 离线评测"）。rerank spec 同时明确了边界决策：受控实验不进入生产 Search 合同，评测必须是独立能力。

本设计的硬约束：**不进行任何人工标注**。因此评测数据必须来自自带 query→相关段落标注（qrels）的公开数据集。

## 2. 目标

本设计完成后，应能回答：

1. 当前版本在中文检索任务上 Recall@5/@10、MRR@10、nDCG@10 是多少？
2. vector-only / FTS-only / RRF 混合 / 混合+Rerank 四种通道组合各自表现如何？混合的增益是否成立？
3. 段落级检索与长文档（分块后）检索两个场景分别表现如何？
4. 更换 chunker 版本、分块参数、检索参数或 Embedding 模型后，指标升还是降？

## 3. 非目标

- 不做线上评测、A/B 实验或流量分流；不改 `SearchResponse` / Search API 的公开合同。
- 不评测答案生成质量（琅嬛不生成答案）；不引入 RAGAS / LLM-as-judge 类依赖 LLM 裁判的指标（首期，全部指标基于确定性 qrels 计算）。
- 不把评测能力嵌入生产服务或 `internal/` 主链路；评测是独立二进制。
- 不在仓库内提交任何评测语料正文（体积与许可再分发原因，见 5.3）。
- 不做英文或其他语言轨道（首期只做中文；架构允许多数据集，后续可加）。

## 4. 数据集选型

### 4.1 候选与决策

| 候选 | 标注 | 许可证 | 决策 |
|---|---|---|---|
| MIRACL-zh（HF `miracl/miracl` + `miracl/miracl-corpus`） | 人工标注 passage 级 qrels，query 来自真实搜索需求 | Apache-2.0（语料源自 Wikipedia，CC BY-SA） | **采用**，主数据集（两个轨道） |
| DuRetrieval（DuReader-retrieval，HF `C-MTEB/DuRetrieval`） | 搜索引擎真实 query + 段落相关性标注 | Apache-2.0（baidu/DuReader） | **暂缓**：HF 版仅 parquet（需引入 arrow 重依赖）且原始语料数 GB；首版不为此加依赖，后续可选接入 |
| T2Retrieval（HF `C-MTEB/T2Retrieval`） | 分级相关性标注 | 数据卡未标注 license | 暂缓；待确认许可后作为可选轨道 |
| MultiHop-RAG / CRAG / CRAG-MM | 多跳证据链 / 端到端生成 | 英文为主、考生成侧 | 不采用（后续若做多跳证据评测再评估） |
| BEIR / MS MARCO | 英文检索标注 | 开放 | 不采用（语言不符） |

选择依据：MIRACL-zh 同时满足"中文、人工/真实标注、Apache-2.0、jsonl.gz/tsv 原始文件（零解析依赖）"四个条件，且单数据集即可覆盖双轨道（段落 + 长文档）。DuRetrieval 的网络搜索域价值留作后续扩展——接入前提是找到 jsonl 原始分发或团队接受 parquet 依赖。

### 4.2 MIRACL-zh 关键事实（已核实）

- 中文子集规模：约 493 万 passage / 124.6 万文章；全语言语料约 16.2 GB，需要按子集获取。
- passage 记录字段：`docid`、`title`、`text`。
- **`docid` 格式为 `文章ID#段落号`（如 `7#0`、`39#5`），同前缀 passage 来自同一篇 Wikipedia 文章**——这是长文档轨道（6.2）聚合的依据。
- qrels 人工标注 passage 级相关性，含 train/dev split；评测使用 dev split。

### 4.3 VCSUM（2026-08-26 新增，无结构连续文本轨道）

VCSUM（`github.com/hahahawu/VCSum`，ACL 2023 Findings，MIT）是 239 场真实中文会议转写（230+ 小时，B 站视频 ASR），带**人工标注的话题切分**（`eos_index`，每段闭区间结束下标）与段级标题/摘要。接入动机：MIRACL-zh 语料是结构化维基百科，heading 策略的主场；VCSUM 代表"会议转写/ASR 口语"这类**无结构连续长文本**，是分块策略（heuristic/滑窗兜底）的弱势场景，用于回答"当前 chunker 在此类语料上的真实损耗"与"语义分块是否值得投入"。

关键事实与构造规则（已核实）：

- 原始文件（GitHub raw，`prepare -dataset vcsum` 自动下载约 30MB 到 `.eval-data/cache/vcsum/`）：`overall_context.txt`（每行一场会议：`id`/`eos_index`/`context`（逐 utterance 句子列表）/`speaker`）与 `short_{train,dev,test}.txt`（每行一个话题段：`id` 形如 `13_0`、`agenda` 段标题、`discussion` 段摘要、`context` 段内 utterance）。
- **数据完整性门槛**：只有 `eos_index` 切分与 short_* 段记录逐 utterance 完全一致的会议进入语料（实测 227/239；其余 12 场整场剔除，不做修补）。
- Track A：每个**人工话题段**一份短文档（docid `vcsum-m<会议>#<段>`，标题用 `agenda`），隔离分块变量；
  Track B：每场**完整会议**一份长文档（docid `vcsum-m<会议>`，utterance 为段落），覆盖分块+父子+检索全链路。
- query 集：**仓库内人工撰写资产** `cmd/langhuan-eval/vcsum_queries.json`（一段一问，基于该段 `agenda`+`discussion` 改写为自然问句；取前 30 场对齐会议的 139 个话题段）。query 是 benchmark 定义的一部分，修改需在 PR 中说明理由。
- gold 判定与 MIRACL 轨道一致：检索结果（track-b 为父块+命中子块拼接）与 gold 段文本的字符 bigram 包含率 ≥ 阈值。
- 入口：`make eval-vcsum`（配置 `eval.config.vcsum*.yaml`，本地文件不进 git）。
- **变体（oracle 实验）**：`prepare -dataset vcsum -vcsum-variant heading|heading-neutral|heading-llm` 在话题段首注入标题（真实 agenda / 中性 `话题段N` / LLM 生成），产物写入 `.eval-data/vcsum-heading*/`，用于隔离「边界对齐」与「标题进 ContextHeader」两个变量（结果见 RETRIEVAL_BENCHMARK.md §4.5/§4.6：边界 ±2pp 内，收益在上下文头；7B 生成版仅吃到 FTS 增益 12%）。heading-llm 需要 `-llm-base-url/-llm-model`（默认本地 Ollama qwen2.5:7b-instruct），生成结果按段 sha 缓存于 `cache/vcsum-llm-titles/`，重跑零成本。

## 5. 数据集设计

### 5.1 双轨道

**Track A - 段落检索**（测 Embedding / FTS / RRF 本身，排除分块变量）：

- 每个 passage 作为一份独立 TXT 文档导入琅嬛（短 passage 通常单 chunk）。
- 语料构成：采样 query 的全部 gold passage + 从语料池确定性随机抽取的干扰 passage。
- 规模：MIRACL-zh 200 query，语料约 5,000 passage；DuRetrieval 同规模。合计约 400 query / 10,000 文档。

**Track B - 长文档检索**（测分块 + 父子 + 检索全链路）：

- 仅 MIRACL-zh：把同一文章（`docid` 同前缀）的全部 passage 按段落号顺序聚合为一篇 Markdown 长文档（标题用文章 `title`，段落间以空行分隔）。
- **变体（2026-08-26）**：`prepare -miracl-variant simplified` 按 OpenCC 单字表把语料转简体（表缓存于 cache/opencc，sha 进 manifest），用于测繁简归一化收益——结论无收益已关闭，见 RETRIEVAL_BENCHMARK.md §4.7。
- 使用与 Track A 相同的 query 与 gold passage；gold 判定从"文档级"降为"chunk 级文本重叠"（5.2）。
- 规模：约 200–500 篇长文档（由采样 query 涉及的文章数决定）。

### 5.2 命中判定：gold passage 文本级（本设计最关键决策）

**不把 qrels 绑定到 chunk ID 或 document ID 上做严格相等**，而是文本重叠判定：

- Track A：检索结果的正文（chunk content）与 gold passage 做规范化文本比对，重叠率 ≥ 阈值（默认 0.6）视为命中 gold。
- Track B：检索结果的完整返回内容（父块正文）与 gold passage 比对，同一阈值。

理由：

1. **chunk ID 随 chunker 版本变化**。qrels 若绑 chunk，每次分块演进全部标注作废，评测无法跨版本回归；文本重叠判定让同一数据集在 chunker v3/v4/... 下持续可用。
2. MIRACL 的 passage 切分与琅嬛的 chunker 切分天然不对齐，只有文本级匹配是唯一稳定的对应关系。
3. 文档 ID 在琅嬛侧是导入期生成的 UUID，同样不能作为跨 run 稳定键；harness 在导入时自行维护 `docid → 琅嬛 document_id` 映射，但只用于诊断输出，不参与命中判定。

阈值 0.6 为经验初值，报告需附阈值敏感性说明（0.5/0.6/0.8 三档指标），首份基线后可固化。

### 5.3 数据获取与分发策略

- **评测数据放在仓库根目录的 `.eval-data/`（隐藏目录），整目录加入 `.gitignore`**——语料缓存（约 730MB 原始分片）与采样产物都不进 git；仓库只提交下载转换代码与 `manifest.json` 的生成逻辑（manifest 随数据集落地在 `.eval-data/` 内）。
- 下载默认走**国内镜像 `https://hf-mirror.com`，失败自动回退直连 `https://huggingface.co`**；两端点均可在 eval 配置 / prepare flags 覆盖。
- MIRACL 语料为 10 个 `docs-N.jsonl.gz` 分片：prepare 一次性下载全部到 `.eval-data/cache/`（含 sha256 校验），之后重复 prepare 命中缓存不再联网。
- 确定性：固定 seed、固定采样算法（query 采样用 seeded shuffle；干扰项用 FNV 哈希过滤 + 哈希序截断，与文件内行顺序无关）；同版本原始文件重复 prepare 产出逐字节一致，保证任何报告可复现。

## 6. 检索通道对比设计

### 6.1 现状

Generation `RetrievalConfig` 已含 `vector_top_k` / `keyword_top_k` / `final_top_k` / `rrf_k`；`searchOptionsFromGeneration`（`internal/application/service/search.go:432`）读取时要求 `vector_top_k` 与 `keyword_top_k` 均在 `minRetrievalTopK(1)..maxCandidateTopK(1000)` 区间——**当前 `0` 是校验错误，不存在"禁用单路"语义**。`SearchInput` 已支持可选的 `vector_top_k` / `keyword_top_k` / `final_top_k` 请求级覆盖。

### 6.2 语义扩展：`0 = 禁用该路`（本设计对主链路的唯一改动）

- `vector_top_k=0` 表示不发起向量召回；`keyword_top_k=0` 表示不发起 FTS 召回；**两路同时为 0 仍返回校验错误**。
- 单路禁用时 RRF 融合退化为该路排序的直通（分数语义不变，仍是 RRF 分数形式）。
- 语义对 Generation 配置与 SearchInput 请求级覆盖一致生效；`final_top_k` 维持 ≥ 1 不变。
- 这是**配置语义扩展，不是实验参数**：不新增字段、不新增开关、不改变响应结构，与 rerank spec"不污染生产 Search 合同"的既有决策一致；顺带让"FTS-only / 向量-only 知识库"成为生产可用配置。
- 实施为独立小提交，带正负集成测试（见 10.1）。

### 6.3 评测矩阵

| 组合 | vector_top_k | keyword_top_k | rerank |
|---|---|---|---|
| vector-only | 50 | 0 | 关 |
| fts-only | 0 | 50 | 关 |
| hybrid（默认，对照生产） | 50 | 50 | 关 |
| hybrid+rerank | 50 | 50 | 开（未配置 rerank provider 时输出 N/A 并注明） |

通道切换全部通过 SearchInput 请求级覆盖实现，**同一 active Generation 内完成四组查询**，不重复建索引；Generation 自身配置保持生产默认。top_k=50 依据 `maxFinalTopK=50` 上限取满候选。

## 7. Harness 设计

### 7.1 形态与位置

- 新增独立入口 `cmd/langhuan-eval`（Go 标准库 + 现有依赖 `gopkg.in/yaml.v3`，无新增第三方库；MIRACL 原始文件为 jsonl.gz/tsv，不需要 parquet 依赖）。
- 指标实现（Recall/MRR/nDCG、文本重叠）、REST 客户端、standalone 拉起、报告生成全部收在 `cmd/langhuan-eval` 单包内，按职责分文件（`metrics.go` / `apiclient.go` / `server.go` / `run.go` / `report.go` / `miracl.go` / `hf.go`）。
- 评测配置为独立文件 `eval.config.yaml`（gitignore，模板 `eval.config.example.yaml` 入库；另有可提交的 `eval.config.smoke.yaml` 用于离线冒烟）。

### 7.2 子命令

**`langhuan-eval prepare --dataset all|miracl-zh|duretrieval [--mirror URL]`**

下载 → 确定性子采样 → 落地本地标准格式：`corpus.jsonl`（docid/title/text/track）、`queries.jsonl`（query_id/text）、`qrels.jsonl`（query_id/docid/relevance）、`manifest.json`。

**`langhuan-eval run --config eval.config.yaml [--mode standalone|remote]`**

1. 启动被测系统：
   - `standalone`（默认）：以子进程拉起琅嬛单二进制，临时 SQLite 数据目录，结束即销毁——顺带持续验证单二进制交付质量；
   - `remote`：`--base-url` 指向已运行实例（复用已建索引，适合快速重跑查询矩阵）。
2. REST 引导（沿用 `cmd/langhuan/*_e2e_test.go` 的既有模式）：注册首用户（自动 platform_admin）→ 建 workspace → 注册 embedding provider/model → 建知识库（分块配置可由 eval config 指定，用于 chunker 对比）→ 逐轨道导入语料（文件创建接口）→ 轮询至 active Generation ready。
3. 执行查询矩阵：对每轨道 × 每通道组合 × 每 query 调单库 search，记录 ranked 结果。
4. 计算指标 → 写报告。

`eval.config.yaml`（独立文件，不进 `config.yaml`）：目标地址、embedding provider 配置、可选 rerank provider、轨道开关、top_k 矩阵、文本重叠阈值、输出目录、HF 镜像端点。

**`langhuan-eval mock-embedding`**：本地确定性 OpenAI-compatible `/embeddings` 服务（同一文本永远返回同一向量）。它不提供语义能力，用途是**在没有真实 Embedding API 的环境（CI、离线开发机）端到端验证评测全链路**；此时指标值无语义意义，只验证流程与确定性。`make eval-smoke` 用它跑离线冒烟。

### 7.3 Embedding 与成本

- 评测需要真实 Embedding API（Ollama 本地或云 API，走琅嬛既有 provider 注册流程）；eval config 显式声明，报告记录模型五元组（provider/模型名/维度/参数/config hash）指纹。
- 首期规模：两轨道合计约 1 万 passage 级 chunk，单次全量 run 的 embedding 成本可控；`remote` 模式 + Generation 缓存可避免重复嵌入。
- 不同 Embedding 模型的对比报告本身即为产出之一。

## 8. 指标

| 指标 | 定义 | 输出 |
|---|---|---|
| Recall@5 / Recall@10 | 前 K 结果覆盖 gold passage 的 query 比例 | 每轨道 × 每通道 |
| MRR@10 | 首个命中 gold 的倒数排名均值 | 同上 |
| nDCG@10 | 二值相关性折损累计增益 | 同上 |

- 全部指标基于确定性 qrels 计算，无 LLM 参与。
- 琅嬛检索为确定性 RRF：**同一数据集 + 同一 Generation + 同配置重复 run，指标必须逐位一致**（验收项）。
- 报告附每 query 命中明细（命中排名 / 未命中），供失败分析。

## 9. 报告与可复现性

- 输出目录：`docs/eval/<date>_<dataset>@<manifest-short-sha>_<model-name>/`，含 `report.md`（人读）与 `metrics.json`（机器可 diff）。
- 报告指纹头（缺一不可）：数据集名 + manifest sha256、琅嬛二进制 version（build info）、chunker_version 与分块参数、embedding 五元组、rerank 模型（或 N/A）、通道矩阵每格的实际 top_k/rrf_k、文本重叠阈值。
- `make eval`：prepare（缓存检测）+ run（standalone）一键执行。
- 变更约定（写入 CONTRIBUTING/AGENTS）：凡修改 chunker、检索融合、默认检索参数的 PR，须附新旧 `metrics.json` 对比或说明为何不适用。

## 10. 阶段任务

- **T1**（已交付）主链路：`top_k=0` 禁用语义 + 正负测试（`searchOptionsFromGeneration` / `validateSearchRequest` / 单库与多库检索跳过禁用路）。
- **T2**（已交付）`langhuan-eval prepare`：MIRACL-zh 双轨下载、确定性子采样、manifest。
- **T3**（已交付）指标包：Recall/MRR/nDCG + 文本重叠命中判定，表驱动单测。
- **T4**（已交付）`langhuan-eval run`：standalone 拉起、REST 引导、导入、轮询、查询矩阵、报告输出。
- **T5**（已交付）`make eval` / `make eval-prepare` / `make eval-smoke`、`.gitignore`、`eval.config.example.yaml`、spec 操作指南（§13/§14）。
- **T6（可选后置）**：DuRetrieval 第二域轨道、T2Retrieval 分级相关性、chunker 版本对比专项、MultiHop 证据链评测。

## 11. 验收标准

1. 干净机器（Go + 网络）上 `langhuan-eval prepare` 可完成，同 manifest 重复执行产出 sha256 一致。
2. `langhuan-eval run --mode standalone` 全流程零人工干预；同配置重复 run 指标逐位一致。
3. 报告能区分 6.3 矩阵四格（rerank 未配置时为 N/A 并注明原因），指纹头字段完整。
4. Track B 在默认父子分块（4096/384）下正常完成，证明长文档全链路可评测。
5. T1 语义扩展：两路同 0 被拒绝；vector-only 结果与"仅向量召回"一致（与关闭 FTS 的等价查询对拍）；既有 Generation/检索测试全部通过。
6. `go test ./...`、`go vet ./...`、`gofmt` 通过；评测数据与凭证不进 git（`git status` 干净）。

## 12. 风险与开放问题

- **HF 下载体积**：miracl-corpus zh 全量 10 分片约 730MB。prepare 一次性下载到 `.eval-data/cache/`（sha256 校验、断点重下），二次运行命中缓存不再联网；镜像失败自动回退直连。
- **parquet 依赖**：已消解——MIRACL 原始分发即 jsonl.gz/tsv，全程零解析依赖；DuRetrieval 因仅 parquet 分发而暂缓（§4.1）。
- **（已修复）评测冒烟暴露的两个 standalone 既有缺陷**：其一，SQLite 连接开启 `foreign_keys` 但 `knowledge_bases` 的前向复合 FK（指向 file_tree_nodes / knowledge_base_index_generations）无 DEFERRABLE，KB 创建事务按 kb 行 → root → generation 顺序插入立即违约（HTTP 500）；修复为 WorkspaceTxRunner 在非 PG 事务内执行 `PRAGMA defer_foreign_keys = ON`，对齐 PG DEFERRABLE 语义，含集成回归测试。其二，`modernc.org/sqlite/vec` 此前只在测试文件空导入，发布二进制未链接 vec 扩展，standalone 向量检索报 `no such function: vec_f32`；修复为 sqlitedialect 生产代码空导入。两项均为评测 harness 端到端冒烟直接发现的真实产品缺陷，佐证了 standalone 拉起式评测的价值。
- **（已修复）FTS 查询侧停用词过滤（首份基线的核心发现）**：基线实测 `fts_only` 在 MIRACL-zh 疑问句上 recall@10=0——gse 把"埃及有哪些民族？"切为 `[埃及 有 哪些 民族 ？]` 后全 token AND，"哪些/？/有"在正文中永不齐备。修复：新增 `FilterFTSQueryTokens`（标点/单字虚词/疑问填充词过滤，词表刻意保守），SQLite FTS5 MATCH 表达式与 PG plainto_tsquery 查询串共用同一过滤（PG 模式同样装配 gse 分词器）。修复后 fts_only 从 0 → 0.131，track-a `hybrid` recall@10 首次严格高于 vector_only（0.9826 vs 0.9799）——RRF 混合检索的核心架构假设由评测闭环验证成立。停用词表扩充必须附 `make eval` 新旧对比。
- **报告未命中归因（v1.2.0 追加）**：主阈值下每个通道组合的未命中 query 拆分为「gold 文档已召回但文本重叠不足（分块/匹配损耗）」与「gold 文档未召回」两类（gold 文档按导入标题的 `[docid]` 标记识别）。bge-m3 基线的 Track B 结论：6 个 vector 未命中中 5 个属前者——分块/匹配是长文档轨道的主要损耗模式，为 chunker 演进提供了靶子。
- **离线冒烟 fixture（v1.2.0 追加）**：`cmd/langhuan-eval/testdata/micro` 入库微型数据集（12 query / 90 段落 / 44 长文档，自全量采样派生），`make eval-smoke` 不再依赖 HF 下载，可在 CI 中端到端防回归（上述 standalone 双缺陷即此类问题）。
- **chunker 参数实验记录（v1.2.0，含阴性结果）**：归因下钻显示 track-b 的 6 个 vector 未命中中 5 个是「gold 文档已召回、但含 gold 段落的父块没进 top-10」（如"意大利汽车品牌"命中意大利文章的其它章节），且 gold 段落多为繁体中文（语料繁简混杂）。据此跑了两组单变量实验（bge-m3，仅 track-b，其余同指纹）：
  - E2 `parent_chunk_size` 4096→8192：recall@10 0.7918→0.7732（**未提升，证伪父块切分主因假设**）；MRR 0.9199→0.9392、rerank ndcg 0.7952→0.7989 小幅变好。
  - E3 `child_chunk_size` 384→256：recall@10 0.7918→0.7942、hybrid 0.7962、ndcg 0.7975，三者最优但仅 +0.4~0.5pp；6 个未命中在两组实验中完全不变。
  - **结论**：分块参数不是剩余差距的有效杠杆（±0.5pp 不构成改默认合同的理由，维持 4096/384）；track-b 的有效杠杆是 rerank（已交付）；下一前沿是**繁简归一化**（索引与查询侧统一简体，FTS 与匹配双双受益——gold 繁体段落是未命中主因之一）。实验用 `chunking:`/`tracks:` 配置项复现（见 §13）。
- **文本重叠阈值**：0.6 初值可能偏严/偏松，首份报告的三档敏感性数据用于校准；阈值进 eval config，不硬编码。
- **MIRACL qrels 的 train/dev 划分**：使用 dev split 避免与（未来可能的）微调数据重叠；若 dev gold passage 覆盖不足 200 query，回落到 train split 抽样并在 manifest 标注。
- **DuRetrieval 语料规模**（百万级 passage）：已随 §4.1 决策暂缓；若未来接入，同 prepare 子采样策略处理。

## 13. 评测操作指南（实现完成后如何跑）

### 13.1 首次准备（一次性）

```bash
# 1. 生成评测配置（真实评测需要可用的 OpenAI-compatible Embedding 端点）
cp eval.config.example.yaml eval.config.yaml
#    编辑 embedding.base_url / model_name / dimensions / api_key（或 api_key_file）

# 2. 下载并采样数据集（默认 MIRACL-zh：200 query + 双轨语料；首次约 730MB，走镜像）
make eval-prepare
#    等价：go run ./cmd/langhuan-eval prepare
#    可调参数：--queries 200 --distractors 4800 --distractor-articles 300 --seed 20260824
```

产物全部落在仓库根 `.eval-data/`（gitignore）：`cache/`（原始分片，二次运行不再联网）与 `miracl-zh/`（queries/qrels/双轨语料/manifest.json）。

### 13.2 执行评测

```bash
make eval
#    等价于：数据集缺失时自动 prepare + go run ./cmd/langhuan-eval run -config eval.config.yaml
```

流程自动完成：随机空闲端口拉起 standalone 琅嬛实例（SQLite 临时库，`.eval-data/runtime/<id>/`，含 server.log）→ 注册用户/workspace/embedding 模型 → 双轨导入语料并等待 ready → 四格通道矩阵逐 query 检索 → 计算 Recall@5/@10、MRR@10、nDCG@10（三档阈值敏感性）→ 写报告。

报告输出在 `docs/eval/<时间戳>_miracl-zh_<模型名>/`（`report.md` + `metrics.json`），目录含完整指纹，不互相覆盖。

### 13.3 常用变体

```bash
# 连接已运行的实例（复用已建索引，快速重跑查询矩阵；要求干净环境）
go run ./cmd/langhuan-eval run -config eval.config.yaml   # server.mode: remote + base_url

# 离线冒烟（无真实 API 环境，验证全链路与确定性；指标无语义意义）
make eval-smoke

# 更换 Embedding 模型对比：改 eval.config.yaml 的 model_name/dimensions 后重新 make eval
```

### 13.4 变更回归约定

凡修改 chunker、检索融合（RRF）、默认检索参数或 embedding 链路的 PR，须附新旧 `metrics.json` 对比（或说明为何不适用）。对比前提：两次 run 的指纹一致（数据集 manifest sha256、分块、embedding 五元组、矩阵参数）；指纹一致时，琅嬛的确定性 RRF 保证重复 run 指标逐位一致，任何差异都来自代码变更。

## 14. 结果解读（怎么看报告）

`report.md` 的结构：**指纹表**（复现所需全部信息）→ 每轨道一张**通道矩阵表**（recall@5/recall@10/mrr@10/ndcg@10）→ **阈值敏感性表**（ndcg@10 @0.5/0.6/0.8）→ 解读指引。机器对比用 `metrics.json`（结构化、可 diff）。

按顺序回答四个问题：

1. **混合检索的增益是否成立**（track-a：`hybrid` vs `vector_only` / `fts_only`）：这是琅嬛核心架构假设的直接验证。健康表现是 `hybrid` 的 recall@10 不低于两路单用的较大者；若 hybrid 反而低于某单路，说明 RRF 融合或候选深度有问题。
2. **分块全链路的损耗有多大**（对比 track-a 与 track-b 的同通道指标）：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回伤害越大——这是调整 `parent_chunk_size`/`child_chunk_size`、演进 chunker 版本时的主要量化依据。
3. **重排值不值得开**（`hybrid_rerank` vs `hybrid`）：差值就是 rerank 模型在自有管线上的真实增益；未配置 rerank 模型时该行显示 N/A 及原因。
4. **结论是否可信**（阈值敏感性表）：不同阈值的 ndcg 差异过大（例如 @0.5 与 @0.8 相差 >15 个百分点）时，命中判定偏松或偏严，先校准 `overlap.threshold` 再下业务结论；两次 run 对比必须核对指纹表一致。

**典型读数参考**（MIRACL-zh，真实 embedding 模型）：FTS 查询侧停用词过滤落地后，中文疑问句的 fts_only recall@10 约 0.12~0.13（词法通道对问句天然偏弱，关键词型 query 才是其主场）；track-a 的 hybrid recall@10 应**严格不低于** vector_only（混合增益成立）；track-b 相对 track-a 的衰减主要来自长文档干扰项增多与分块边界。绝对数值无及格线，**一切以同指纹的相对比较为准**。


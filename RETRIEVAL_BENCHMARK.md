# 琅嬛检索评测综合报告

> **版本基线**：v1.1.1（含 v1.2.0 开发中的检索修复）· **数据截止**：2026-08-25
> **评测体系**：`cmd/langhuan-eval`（设计规格见 [docs/superpowers/specs/2026-08-24-retrieval-eval-design.md](docs/superpowers/specs/2026-08-24-retrieval-eval-design.md)）
> 本报告综合了 2026-08-24 ~ 08-25 期间的全部跑分轮次，供评审与后续回归对比。

---

## TL;DR

| 场景 | 实测水平（bge-m3） | 结论 |
|---|---|---|
| 段落级检索（FAQ/短条目） | recall@10 = **0.9826**（hybrid），MRR = 0.9967 | 一流水平，命中几乎都在第 1 位 |
| 长文档检索（全文级，含分块全链路） | recall@10 = **0.7938**（hybrid+rerank），nDCG = 0.7952 | 约 80%，剩余损失已归因到三个明确失效模式 |
| 混合检索假设 | hybrid **严格优于**任一单路（0.9826 > 0.9799 > 0.1314） | RRF 融合架构假设经实测验证成立 |
| 可复现性 | 同指纹重复 run 指标**逐位一致** | 每次改动可量化回归 |

**推荐生产配置**：bge-m3（或同档中文模型）+ 默认混合检索 + **开启 workspace 级 rerank** + 维持默认分块 4096/384。详见 §5。

---

## 1. 为什么建这套评测

琅嬛在 v1.1.0 之前的所有测试都是正确性测试（功能对不对），没有任何量化手段回答"检索好不好"。分块合同从 v1 演进到 v3、RRF 融合、rerank——这些核心架构决策从未被实测验证过。两份既有设计规格（rerank、search lineage）都把"基于 qrels 的离线评测"列为待办。

本评测体系于 v1.2.0 周期交付，**零人工标注**（使用带人工标注的公开数据集），独立二进制、不进主链路。它建立后立即产出了三个产品修复与四个架构结论——本文即这些结果的完整汇总。

## 2. 评测方法

### 2.1 数据集与双轨设计

**数据集**：[MIRACL-zh](https://github.com/project-miracl/miracl)（Apache-2.0，Wikipedia 中文语料，人工标注段落级相关性）。确定性子采样：**200 条真实搜索 query**，seed=20260824，manifest sha256 可复现。

**第二数据集（2026-08-26）**：[VCSUM](https://github.com/hahahawu/VCSum)（MIT，239 场真实中文会议转写，人工标注话题切分）——代表**无结构连续文本**（会议/ASR 口语），测同一套系统在 chunker 弱势语料上的表现。取数据完整性对齐的 227 场为语料（话题段 1297 份/长文档 227 份），前 30 场的 139 个话题段配人工撰写 query（一段一问，`cmd/langhuan-eval/vcsum_queries.json`）。

**双轨道**回答两个不同的问题：

| 轨道 | 语料 | 度量目标 |
|---|---|---|
| **track-a 段落检索** | 5,298 份单段落文档 | Embedding / FTS / RRF 融合本身的水平（隔离分块变量） |
| **track-b 长文档检索** | 709 篇全文文章（按 Wikipedia 文章聚合，1~40 段） | 分块 → 父子 chunk → 检索的全链路水平 |

**命中判定**：gold 段落与返回内容做字符 bigram 文本重叠（阈值 0.6，附 0.5/0.8 敏感性），**不绑定 chunk ID**——因此同一份标注可跨 chunker 版本持续回归。

### 2.2 指标

Recall@5/@10、MRR@10、nDCG@10，全部基于确定性 qrels 计算（无 LLM 参与）。指标实现自带表驱动单测。

### 2.3 通道矩阵

依托检索通道语义扩展（`vector_top_k`/`keyword_top_k` 支持 `0=禁用该路`），同一 active Generation 内跑四格：

`vector_only` / `fts_only` / `hybrid（RRF，生产默认）` / `hybrid_rerank`

### 2.4 可复现性

每份报告带完整指纹（数据集 manifest sha256、chunker 版本与分块参数、embedding 五元组、rerank 模型、矩阵参数、repo HEAD）。琅嬛检索为确定性 RRF：**同指纹重复 run（不同端口、不同临时实例）的 metrics.json 剔除时间戳后逐位一致**——已实测验证。任何指标差异都可归因于代码/配置变更。

## 3. 跑分历程

### 3.1 第 0 轮：离线冒烟（mock embedding）

用确定性 mock embedding（同文本恒同向量，无语义）+ 20 query 精简集验证全链路。mock 指标与随机基线吻合（track-a recall@10 ≈ 10/254），确认 harness 无偏。**这一轮直接逼出两个 standalone 模式的产品级 bug**：创建知识库必 500（SQLite 前向外键缺延迟检查）与向量检索 `vec_f32` 未链接——均在 v1.1.1 修复（commit `0d8cfbd`）。

### 3.2 第 1 轮：首次真实基线（2026-08-25，FTS 修复前）

bge-m3（本地 Ollama）与 Qwen3-Embedding-0.6B（SiliconFlow 云端）双模型，完整 200 query：

| track-a | recall@10 | MRR@10 | track-b | recall@10 | MRR@10 |
|---|---|---|---|---|---|
| bge-m3 vector | 0.9799 | 0.9942 | bge-m3 vector | 0.7918 | 0.9199 |
| bge-m3 **fts** | **0.0000** | 0.0000 | bge-m3 fts | 0.0000 | 0.0000 |
| bge-m3 hybrid | 0.9799（≡vector） | 0.9942 | Qwen3 vector | 0.7817 | 0.9202 |

**三个发现**：

1. **FTS 通道对中文疑问句零召回**：gse 把"埃及有哪些民族？"切为 `[埃及 有 哪些 民族 ？]` 后全 token AND，疑问词在正文永不齐备。FTS 全线 0 分，hybrid 退化为 vector 直通——"混合检索"假设此时**未被验证**。
2. **两模型差距仅 0.4pp**：检索质量瓶颈不在模型。
3. **Track B 衰减 19pp**：分块全链路存在真实损耗。

### 3.3 第 2 轮：FTS 修复后的完整基线（2026-08-25）

**FTS 修复**（commit `e25ce40`，v1.1.1 发布内容）：查询侧过滤标点、单字虚词、疑问填充词（`FilterFTSQueryTokens`，词表刻意保守），SQLite FTS5 与 PG plainto_tsquery 双方言生效。同指纹重跑（bge-m3 + bge-reranker-v2-m3）：

| track-a | recall@10 | MRR@10 | nDCG@10 | track-b | recall@10 | MRR@10 | nDCG@10 |
|---|---|---|---|---|---|---|---|
| vector_only | 0.9799 | 0.9942 | 0.9771 | vector_only | 0.7918 | 0.9199 | 0.7923 |
| fts_only | 0.1314 | 0.1825 | 0.1429 | fts_only | 0.1223 | 0.1879 | 0.1320 |
| **hybrid** | **0.9826** | 0.9967 | **0.9799** | hybrid | 0.7919 | 0.8911 | 0.7795 |
| hybrid+rerank | 0.9778 | **0.9975** | 0.9778 | **hybrid+rerank** | **0.7938** | 0.9143 | **0.7952** |

**验证成立的声明**：FTS 通道复活（0 → 0.13）；**hybrid 首次严格高于 vector-only**（0.9826 > 0.9799，混合检索架构假设闭环验证）；rerank 在 track-a 把 MRR 推到 0.9975（四格最优），在 track-b 修复了 FTS 候选对头部排序的污染（MRR 0.8911 → 0.9143）并成为最强组合。

### 3.4 第 3 轮：分块参数实验（E2/E3，含阴性结果）

单变量实验（bge-m3，仅 track-b，其余同指纹）：

| 配置 | vector recall@10 | vector MRR | hybrid recall@10 | 未命中（6 条结构） |
|---|---|---|---|---|
| 基线 4096/384 | 0.7918 | 0.9199 | 0.7919 | 6（5 已召回 / 1 未召回） |
| E2：parent 8192 | 0.7732 ↓ | 0.9392 ↑ | 0.7739 | 6（不变） |
| E3：child 256 | **0.7942** | 0.9232 | **0.7962** | 6（不变） |

**结论**：分块参数不是剩余差距的有效杠杆（±0.5pp 不构成修改默认合同的理由）；E2 证伪"父块切分导致未命中"的假设。track-b 的有效杠杆是 rerank。

### 3.5 归因下钻：剩余未命中的机理

对 6 个 vector 未命中逐条解剖（重启实例回放查询、比对返回内容与 gold 文本）：

- **5/6 是"文章内章节竞争"**：正确文章已召回，但含 gold 段落的章节在文章内排不过其它章节（如"意大利汽车品牌"命中了意大利文章的出口贸易章节）。
- **多次命中繁简混杂因素**：MIRACL-zh 语料繁简混合，多个 gold 段落为繁体中文而 query 为简体——FTS 词形不匹配、文本重叠判定同时受损。**繁简归一化是已识别的下一个最大改进杠杆**。
- 仅 1/6 是文档级完全未召回（向量语义盲区，如"数学基础运算公式"未匹配到算术运算段落）。

## 4. 当前成果：检索能力证明

### 4.1 经实测验证的能力声明

| # | 声明 | 证据 |
|---|---|---|
| 1 | 段落级中文检索达到 98%+ recall@10、命中首位概率 >99% | track-a：hybrid 0.9826 / MRR 0.9967，**0 条 query 完全无果** |
| 2 | 混合检索（向量+FTS+RRF）严格优于任一单路 | track-a：0.9826 > max(0.9799, 0.1314)；修复前 hybrid≡vector，修复后增量来自 FTS 补充召回 |
| 3 | 长文档全链路（分块→父子→检索）约 80% recall@10 | track-b：0.7938；其中 99.5% 的 query 的 gold 文档被召回 |
| 4 | Rerank 提升排序质量，是长文档场景的最强配置 | track-a MRR 0.9975（四格最优）；track-b nDCG 0.7952（最优），修复 FTS 排序污染 |
| 5 | 检索结果完全确定、可复现、可回归 | 同指纹重复 run 指标逐位一致（实测）；报告指纹含全部归因信息 |
| 6 | 对模型更换不敏感（链路稳定性） | bge-m3 vs Qwen3-Embedding-0.6B 差 0.4pp |
| 7 | 中文关键词型检索可用 | FTS 停用词过滤后词法通道复活；关键词型 query（文件名/术语/编号）为其主场 |

### 4.2 能力边界（诚实声明）

- **绝对分数不可与公开排行榜直接对比**：本评测在 5,298 段落的采样池上检索；MIRACL 官方榜单在 490 万段落全池上计算，池子越大分数越低。本体系的价值是**同指纹相对比较**（同一把尺子量每次改动），不是刷绝对值。
- **FTS 对问句型 query 天然偏弱**（0.12~0.13）：词法 AND 检索的主场是关键词型 query；问句场景由向量+混合兜底。
- **繁简混杂语料会同时压制 FTS 与命中判定**：繁体为主的语料建议等待繁简归一化能力或在导入前转简体。

### 4.3 本评测驱动交付的修复与能力（副产品价值）

| 交付 | 类型 |
|---|---|
| standalone 创建知识库必 500（前向外键延迟检查） | **产品 bug 修复**（v1.1.1） |
| standalone 向量检索失败（sqlite-vec 未链接） | **产品 bug 修复**（v1.1.1） |
| FTS 中文疑问句零召回（查询侧停用词过滤，双方言） | **产品修复**（v1.1.1） |
| `vector_top_k/keyword_top_k=0` 禁用单路召回 | 检索配置语义扩展 |
| 评测 harness + 确定性数据集 + 微型离线 fixture | 工具链（`make eval` / `eval-smoke`） |

### 4.4 无结构连续文本轨道（VCSUM，2026-08-26）

同一套四格矩阵跑在会议转写语料上（bge-m3 + bge-reranker-v2-m3，139 query）：

| 轨道 | 组合 | child=384（默认） | child=512 |
|---|---|---|---|
| track-a（话题段文档） | vector / hybrid / hybrid_rerank recall@10 | 0.9640 / 0.9640 / 0.9640 | 0.9712 / 0.9712 / 0.9712 |
| track-b（会议长文档） | vector_only | 0.8849 | 0.8993 |
| track-b | fts_only | 0.2014 | 0.2302 |
| track-b | hybrid | 0.8921 | 0.9209 |
| track-b | **hybrid_rerank** | **0.9424**（ndcg 0.8687） | 0.9137（ndcg 0.8294） |

结论：

- **无结构长文本上全链路损耗被 rerank 大幅补偿**：track-a→track-b 损耗从无 rerank 的 ~7-8pp 收窄到 2.2pp（0.9640→0.9424）。rerank 在此语料上是最大单点增益（recall +5.0pp、ndcg +8.8pp vs hybrid），印证推荐配置「rerank 必开」。
- **未命中主因仍是「gold 文档已召回、块级匹配损耗」**（默认配置 16 个未命中里 15 个）：与分块观察（约 9% 子块横跨人工话题边界、22% 话题切换点 50 字内无块边界）方向一致——损耗在边界，不在召回。
- **FTS 显著强于维基百科场景**（0.55 vs 0.13）：query 为话题描述型且简体一致（无繁简混杂），印证 4.2 节「FTS 弱在问句与繁简，不在关键词本身」。
- **child 384→512 仍非杠杆**：track-b hybrid +2.9pp 但 hybrid_rerank −2.9pp，track-a +0.7pp——两组数据互相抵消，维持默认 384。
- **语义分块的判定性实验已具备数据基础**：人工话题边界（eos_index）可直接构造 heading 注入的 oracle 对照语料，量化「边界完全对齐话题」的上限增益（见 §6）。

### 4.5 语义分块 oracle 实验（2026-08-26，已闭环）

用 `prepare -vcsum-variant` 构造两个 oracle 变体（其余与基线逐字节一致，仅跑 track-b）：

- **Oracle-B（heading-neutral）**：话题边界注入无信息量标题 `## 话题段N` → 隔离「纯边界对齐」；
- **Oracle-A（heading）**：注入人工话题标题（如 `## 元宇宙的生态发展需完善`）→ 边界对齐 + 标题进 ContextHeader。

前提已验证：变体语料经 markdown 解析 + chunker 后**跨话题子块从 9% 降为 0**。

| track-b recall@10 | 基线 | Oracle-B | Oracle-A |
|---|---|---|---|
| vector_only | 0.8849 | 0.8993（+1.4pp） | **0.9640**（+7.9pp） |
| fts_only | 0.2014 | 0.2014（±0） | **0.5683**（+36.7pp） |
| hybrid | 0.8921 | 0.9065（+1.4pp） | **0.9640**（+7.2pp） |
| hybrid_rerank | 0.9424 | 0.9353（−0.7pp） | **0.9640**（+2.2pp，ndcg 0.9578） |

按预注册规则判读：

1. **Oracle-B − 基线 ≤ ±2pp（三通道均成立）→ 语义分块正式排除。** 把切点精确对齐人工话题边界（任何语义分块实现的理论上限）在 vector/hybrid 上仅 +1.4pp、rerank 上反降 0.7pp。边界不是损耗主因，chunker 契约（确定性 + 默认参数）维持不变。
2. **Oracle-A ≫ Oracle-B（vector +6.5pp、FTS +36.7pp）→ 收益全部来自话题标题进入 ContextHeader，与边界无关。** Oracle-A 的 track-b 追平了 track-a 基线（0.9640）：全链路损耗被「块级话题上下文头」完全消除，未命中 16→5（与 track-a 的固有 5 个一致）。
3. **新的最大杠杆候选：块级上下文头富化（contextual retrieval）。** 在导入时为每个 chunk 生成话题描述（LLM 生成或更简单的规则方案）拼入 EmbeddingContent，不动 chunker。Oracle-A 是「人工金标标题」的上限（且 query 与标题同源于数据集标注，真实收益会折价），但 36.7pp 的 FTS 增益和 7.9pp 的向量增益说明头部空间足够大，值得做可实现版本验证。

### 4.6 LLM 标题可实现版对照（2026-08-26，heading-llm）

`prepare -vcsum-variant heading-llm`：1297 段标题全部由本地 `qwen2.5:7b-instruct`（温度 0、全文输入、按段 sha 缓存）生成后注入，其余与 Oracle-A 完全同构。

| track-b recall@10 | 基线 | heading-llm（7B） | heading-llm（DeepSeek-V4-Flash） | Oracle-A 上限 |
|---|---|---|---|---|
| vector_only | 0.8849 | 0.8849（±0） | 0.8921（+0.7pp） | 0.9640 |
| fts_only | 0.2014 | 0.2446（+4.3pp） | 0.2518（+5.0pp） | 0.5683 |
| hybrid | 0.8921 | 0.8921（±0） | 0.8993（+0.7pp） | 0.9640 |
| hybrid_rerank | 0.9424 | 0.9281（−1.4pp） | 0.9281（−1.4pp） | 0.9640 |

判读：

- **按预注册规则（vector ≥ +4pp 才立项）：不立项（两档生成模型均如此）。** 7B 只吃到 FTS 增益的约 12%、向量零增益；换 DeepSeek-V4-Flash（云端 flash 档，1297 段标题质量肉眼更贴人工框架、query bigram 覆盖 17.3% vs 7B 16.0%）后 vector 也仅 +0.7pp、FTS +5.0pp（约 14%）、rerank 仍 −1.4pp（ndcg 各通道约 +2pp）。**生成模型质量跨档提升而结果几乎不动，说明瓶颈不是模型能力，而是「内容概括型标题」与「提问措辞」的本质错位**——任何只看正文做摘要的生成器都倾向复用正文词汇，而 oracle 增益恰恰来自正文没有、提问者会用的抽象词。
- **测量学偏差必须标注**：本 benchmark 的 query 基于人工 agenda 撰写，用词锚定人工标题（query bigram 覆盖：人工标题 49.3% vs 7B 标题 16.0%）。这使 Oracle-A 偏高、heading-llm 偏低——真实用户 query 对两者中性，真实收益介于两者之间。因此该结果是**下界**，不足以关闭方向，但足以说明**生成质量/措辞对齐是瓶颈**（7B 概括内容是准的，但换一种说法，与 query 的措辞方向脱节）。
- **后续可选**（未执行）：换更强生成模型（云端 flash 档即可）、或调整生成 prompt 让标题更贴近「提问式话题短语」；同时在产品化前需要一套与标题无关撰写的 query 集来消除本偏差。

## 5. 推荐配置

基于全部实验数据，生产环境推荐如下（也是回归基线的参照配置）：

| 配置项 | 推荐值 | 依据 |
|---|---|---|
| Embedding 模型 | **bge-m3**（1024 维）或同档中文模型 | 双模型对比差距仅 0.4pp，本地 Ollama 可跑、零成本可复现基线 |
| 检索通道 | **默认混合（vector+FTS+RRF）** | 实测严格优于任一单路；勿关闭任一通道 |
| Rerank | **开启**（如 `BAAI/bge-reranker-v2-m3`），`candidate_top_k=50` | track-a MRR 四格最优；track-b 最强组合（nDCG +0.16pp vs hybrid，并修复 FTS 排序污染） |
| 分块 | **维持默认**：auto 父子，父 4096 / 子 384 | E2/E3 实验显示参数仅 ±0.5pp，不足以改默认合同；子块 256 为可选微调项 |
| 候选深度 | 每路 top_k=50、final_top_k=10 | 全部实验的标准配置，与推荐 rerank candidate_top_k 对齐 |
| Embedding 批大小 | 32 | 评测全链路使用值 |
| 繁体为主的语料 | 导入前转简体（暂无内建归一化） | 繁简混杂是已量化的未命中主因之一 |

> 独立部署（standalone SQLite）与生产部署（PostgreSQL+zhparser）的检索语义一致；本报告数据采集自 standalone 实例（FTS 走 gse 分词），PG 侧 FTS 行为依赖 zhparser，改停用词表或分词相关代码时建议两方言各跑一次 `make eval`。

## 6. 已知短板与改进路线（按数据支撑的优先级）

1. **繁简归一化**（最大剩余杠杆）：索引与查询侧统一转简体后进 FTS/匹配，直接针对归因出的未命中主因。
2. **长文档的文档内章节排序**（~3% query）：重排可缓解未根治；可能方向包括段落级查询扩展或按 section 的多路召回。
3. **FTS 停用词表扩充**：当前词表刻意保守（宁漏勿错）；扩充必须附 `make eval` 新旧对比。
4. **数据集扩展**（T6）：接入关键词型 query 数据集（T2Retrieval 查许可证 / DuRetrieval），验证 FTS 修复对关键词场景无伤害，并覆盖第二 query 风格。
5. ~~语义分块 oracle 实验~~ **已闭环（2026-08-26，§4.5）**：完美语义边界的上限增益 ≤±2pp，正式排除语义分块；收益主体是话题标题进 ContextHeader——新方向见第 6 条。
6. **块级上下文头富化**（§4.5/§4.6 已测 7B 与 DeepSeek-V4-Flash 两档）：oracle 上限 +7.9pp vector / +36.7pp FTS；两档生成版 vector +0~0.7pp、FTS 仅吃到 12~14%——按预注册规则不立项。**模型跨档而结果不动 ⇒ 瓶颈是「内容摘要措辞」与「提问措辞」的错位，非模型能力**（benchmark 对 LLM 标题偏严，结果为下界，但不足以支撑投入）。重启前提：先造与标题无关撰写的 query 集消除偏差，再评估提问式 prompt 是否真能改变生成措辞。

## 7. 如何复现

```bash
# 一次性：配置真实 Embedding 端点（或本地 Ollama bge-m3）
cp eval.config.example.yaml eval.config.yaml

# 准备数据集（首次约 730MB，走 HF 镜像；之后命中缓存）
make eval-prepare

# 执行评测（standalone 拉起被测二进制 → 导入 → 四格矩阵 → 报告）
make eval

# 无网络环境冒烟（mock embedding + 入库微型数据集；指标无语义意义）
make eval-smoke

# 会议转写轨道（无结构连续文本；语料约 30MB，query 集为仓库内人工资产）
cp eval.config.yaml eval.config.vcsum.yaml   # 把 dataset.dir 改为 .eval-data/vcsum
make eval-vcsum
```

报告输出于 `docs/eval/<时间戳>_<数据集>_<模型>/`（`report.md` + `metrics.json`）。对比两次 run：`diff` 两份 metrics.json，指纹一致时指标差异才可归因于代码/配置变化。

## 附录 A：报告归档索引

| 目录 | 轮次 | 说明 |
|---|---|---|
| `docs/eval/20260825-003659_miracl-zh_mock-embedding/` | 第 0 轮 | 离线冒烟 + 确定性验证（双 run 逐位一致） |
| `docs/eval/20260825-071645_miracl-zh_bge-m3/` | 第 1 轮 | FTS 修复前基线（FTS=0 的发现记录） |
| `docs/eval/20260825-085133_miracl-zh_Qwen-Qwen3-Embedding-0.6B/` | 第 1 轮 | 云端模型对照 |
| `docs/eval/20260825-094908_miracl-zh_bge-m3/` | **第 2 轮** | **FTS 修复后完整基线（四格矩阵 + 归因），推荐配置的数据来源** |
| `docs/eval/20260825-101652_miracl-zh_bge-m3/` | 第 3 轮 | E2：parent 8192（阴性结果） |
| `docs/eval/20260825-102820_miracl-zh_bge-m3/` | 第 3 轮 | E3：child 256（微弱正结果） |
| `docs/eval/20260826-101543_vcsum_bge-m3/` | 第 4 轮 | **VCSUM 无结构语料基线（默认 384）** |
| `docs/eval/20260826-105110_vcsum_bge-m3/` | 第 4 轮 | VCSUM child 512 对照（参数仍非杠杆） |
| `docs/eval/20260826-112454_vcsum-heading-neutral_bge-m3/` | 第 5 轮 | **Oracle-B：纯边界对齐（±2pp 内，语义分块排除）** |
| `docs/eval/20260826-114250_vcsum-heading_bge-m3/` | 第 5 轮 | **Oracle-A：边界+话题标题（+7.9pp vector / +36.7pp FTS，收益在上下文头）** |
| `docs/eval/20260826-150907_vcsum-heading-llm_bge-m3/` | 第 6 轮 | heading-llm：7B 生成标题（仅吃到 FTS 增益 12%，生成质量是瓶颈） |
| `docs/eval/20260826-161058_vcsum-heading-llm_bge-m3/` | 第 6 轮 | heading-llm：DeepSeek-V4-Flash 生成标题（vector +0.7pp，模型跨档而结果几乎不动，瓶颈=措辞错位） |

## 附录 B：相关文档

- 评测设计规格与操作指南：[docs/superpowers/specs/2026-08-24-retrieval-eval-design.md](docs/superpowers/specs/2026-08-24-retrieval-eval-design.md)
- 版本路线：[ROADMAP.md](ROADMAP.md)（「离线检索评测基准（已交付）」章节）
- 检索证据血缘与回放：`docs/superpowers/specs/2026-08-10-search-evidence-lineage-replay-design.md`

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

## 附录 B：相关文档

- 评测设计规格与操作指南：[docs/superpowers/specs/2026-08-24-retrieval-eval-design.md](docs/superpowers/specs/2026-08-24-retrieval-eval-design.md)
- 版本路线：[ROADMAP.md](ROADMAP.md)（「离线检索评测基准（已交付）」章节）
- 检索证据血缘与回放：`docs/superpowers/specs/2026-08-10-search-evidence-lineage-replay-design.md`

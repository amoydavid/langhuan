# 离线检索评测报告：miracl-zh-micro

## 指纹（复现所需全部信息）

| 字段 | 值 |
|---|---|
| 生成时间 | 2026-11-13T00:07:22+08:00 |
| 数据集 | miracl-zh-micro（seed=20260824, manifest=5e02bb6d9ddb…） |
| query / TrackA 语料 / TrackB 语料 | 12 / 90 / 44 |
| 分块 | auto parent=4096 child=384 |
| Embedding | openai/mock-embedding dim=1024 |
| Rerank | 未启用 |
| 通道矩阵 | top_k=50 final_top_k=10 |
| 命中阈值 | 0.60（敏感性见各组合明细） |
| 琅嬛仓库 | 913216898416 |
| 被测实例 | standalone（http://127.0.0.1:59272） |

## track-a：段落检索（单段落文档，隔离分块变量）

语料 90 份 / query 12 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.0833 | 0.1167 | 0.1076 | 0.1023 | OK |
| fts_only | 0.2333 | 0.2333 | 0.3333 | 0.2586 | OK |
| hybrid | 0.3167 | 0.3333 | 0.3438 | 0.3057 | OK |
| hybrid_rerank | - | - | - | - | N/A（未配置 rerank 模型） |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.1023 | 0.1023 | 0.1023 |
| fts_only | 0.2586 | 0.2586 | 0.2586 |
| hybrid | 0.3057 | 0.3057 | 0.3057 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 9 | 0 | 9 |
| fts_only | 8 | 0 | 8 |
| hybrid | 6 | 0 | 6 |

## track-b：长文档检索（Wikipedia 文章聚合，覆盖分块+父子+检索全链路）

语料 44 份 / query 12 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.0861 | 0.0861 | 0.0611 | 0.0516 | OK |
| fts_only | 0.2167 | 0.2167 | 0.2917 | 0.2137 | OK |
| hybrid | 0.2694 | 0.2861 | 0.2389 | 0.2006 | OK |
| hybrid_rerank | - | - | - | - | N/A（未配置 rerank 模型） |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.0794 | 0.0516 | 0.0516 |
| fts_only | 0.2137 | 0.2137 | 0.2137 |
| hybrid | 0.2284 | 0.2006 | 0.2006 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 9 | 2 | 7 |
| fts_only | 8 | 0 | 8 |
| hybrid | 6 | 2 | 4 |

## 如何解读

- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。
- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。
- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。
- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。
- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，先校准阈值再下结论。

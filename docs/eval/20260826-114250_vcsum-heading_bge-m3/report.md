# 离线检索评测报告：vcsum-heading

## 指纹（复现所需全部信息）

| 字段 | 值 |
|---|---|
| 生成时间 | 2026-08-26T11:42:50+08:00 |
| 数据集 | vcsum-heading（seed=20260826, manifest=8461e230ec6a…） |
| query / TrackA 语料 / TrackB 语料 | 139 / 1297 / 227 |
| 分块 | auto parent=4096 child=384 |
| Embedding | openai/bge-m3 dim=1024 |
| Rerank | BAAI/bge-reranker-v2-m3 |
| 通道矩阵 | top_k=50 final_top_k=10 |
| 命中阈值 | 0.60（敏感性见各组合明细） |
| 琅嬛仓库 | eb039a4295ac |
| 被测实例 | standalone（http://127.0.0.1:57719） |

## track-b：会议转写长文档检索（无结构连续文本，覆盖分块+父子+检索全链路）

语料 227 份 / query 139 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.9640 | 0.9640 | 0.9400 | 0.9462 | OK |
| fts_only | 0.5683 | 0.5683 | 0.5647 | 0.5657 | OK |
| hybrid | 0.9640 | 0.9640 | 0.9329 | 0.9409 | OK |
| hybrid_rerank | 0.9640 | 0.9640 | 0.9556 | 0.9578 | OK |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.9750 | 0.9462 | 0.9030 |
| fts_only | 0.5774 | 0.5657 | 0.5468 |
| hybrid | 0.9670 | 0.9409 | 0.9004 |
| hybrid_rerank | 0.9803 | 0.9578 | 0.9146 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 5 | 5 | 0 |
| fts_only | 60 | 6 | 54 |
| hybrid | 5 | 5 | 0 |
| hybrid_rerank | 5 | 5 | 0 |

## 如何解读

- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。
- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。
- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。
- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。
- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，先校准阈值再下结论。

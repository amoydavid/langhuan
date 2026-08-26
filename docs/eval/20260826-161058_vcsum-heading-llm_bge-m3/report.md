# 离线检索评测报告：vcsum-heading-llm

## 指纹（复现所需全部信息）

| 字段 | 值 |
|---|---|
| 生成时间 | 2026-08-26T16:10:58+08:00 |
| 数据集 | vcsum-heading-llm（seed=20260826, manifest=f2dd5415e59e…） |
| query / TrackA 语料 / TrackB 语料 | 139 / 1297 / 227 |
| 分块 | auto parent=4096 child=384 |
| Embedding | openai/bge-m3 dim=1024 |
| Rerank | BAAI/bge-reranker-v2-m3 |
| 通道矩阵 | top_k=50 final_top_k=10 |
| 命中阈值 | 0.60（敏感性见各组合明细） |
| 琅嬛仓库 | ebf52af0120b |
| 被测实例 | standalone（http://127.0.0.1:57076） |

## track-b：会议转写长文档检索（无结构连续文本，覆盖分块+父子+检索全链路）

语料 227 份 / query 139 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.8633 | 0.8921 | 0.7481 | 0.7836 | OK |
| fts_only | 0.2518 | 0.2518 | 0.2482 | 0.2491 | OK |
| hybrid | 0.8705 | 0.8993 | 0.7729 | 0.8040 | OK |
| hybrid_rerank | 0.9209 | 0.9281 | 0.8616 | 0.8781 | OK |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.8078 | 0.7836 | 0.7441 |
| fts_only | 0.2609 | 0.2491 | 0.2419 |
| hybrid | 0.8274 | 0.8040 | 0.7671 |
| hybrid_rerank | 0.9006 | 0.8781 | 0.8376 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 15 | 11 | 4 |
| fts_only | 104 | 5 | 99 |
| hybrid | 14 | 10 | 4 |
| hybrid_rerank | 10 | 7 | 3 |

## 如何解读

- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。
- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。
- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。
- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。
- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，先校准阈值再下结论。

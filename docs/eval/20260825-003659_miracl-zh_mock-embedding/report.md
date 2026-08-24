# 离线检索评测报告：miracl-zh

## 指纹（复现所需全部信息）

| 字段 | 值 |
|---|---|
| 生成时间 | 2026-08-25T00:36:59+08:00 |
| 数据集 | miracl-zh（seed=20260824, manifest=46fcbe6e0b0c…） |
| query / TrackA 语料 / TrackB 语料 | 20 / 254 / 57 |
| 分块 | auto（默认父子） parent=4096 child=384 |
| Embedding | openai/mock-embedding dim=1024 |
| Rerank | <nil> |
| 通道矩阵 | top_k=50 final_top_k=10 |
| 命中阈值 | 0.60（敏感性见各组合明细） |
| 琅嬛仓库 | d5a6e55619ed |
| 被测实例 | standalone（http://127.0.0.1:52215） |

## track-a：段落检索（单段落文档，隔离分块变量）

语料 254 份 / query 20 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.0000 | 0.0417 | 0.0113 | 0.0163 | OK |
| fts_only | 0.0000 | 0.0000 | 0.0000 | 0.0000 | OK |
| hybrid | 0.0000 | 0.0417 | 0.0113 | 0.0163 | OK |
| hybrid_rerank | - | - | - | - | N/A（未配置 rerank 模型） |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.0163 | 0.0163 | 0.0163 |
| fts_only | 0.0000 | 0.0000 | 0.0000 |
| hybrid | 0.0163 | 0.0163 | 0.0163 |

## track-b：长文档检索（Wikipedia 文章聚合，覆盖分块+父子+检索全链路）

语料 57 份 / query 20 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.0000 | 0.1350 | 0.0387 | 0.0526 | OK |
| fts_only | 0.0000 | 0.0000 | 0.0000 | 0.0000 | OK |
| hybrid | 0.0000 | 0.1350 | 0.0387 | 0.0526 | OK |
| hybrid_rerank | - | - | - | - | N/A（未配置 rerank 模型） |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.0638 | 0.0526 | 0.0526 |
| fts_only | 0.0000 | 0.0000 | 0.0000 |
| hybrid | 0.0638 | 0.0526 | 0.0526 |

## 如何解读

- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。
- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。
- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。
- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。
- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，先校准阈值再下结论。

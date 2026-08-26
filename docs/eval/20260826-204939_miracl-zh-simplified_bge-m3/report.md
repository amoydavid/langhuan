# 离线检索评测报告：miracl-zh-simplified

## 指纹（复现所需全部信息）

| 字段 | 值 |
|---|---|
| 生成时间 | 2026-08-26T20:49:39+08:00 |
| 数据集 | miracl-zh-simplified（seed=20260824, manifest=eae0ff9a2633…） |
| query / TrackA 语料 / TrackB 语料 | 200 / 5298 / 709 |
| 分块 | auto parent=4096 child=384 |
| Embedding | openai/bge-m3 dim=1024 |
| Rerank | BAAI/bge-reranker-v2-m3 |
| 通道矩阵 | top_k=50 final_top_k=10 |
| 命中阈值 | 0.60（敏感性见各组合明细） |
| 琅嬛仓库 | 172e5c803478 |
| 被测实例 | standalone（http://127.0.0.1:62070） |

## track-a：段落检索（单段落文档，隔离分块变量）

语料 5298 份 / query 200 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.9545 | 0.9781 | 0.9950 | 0.9765 | OK |
| fts_only | 0.1553 | 0.1601 | 0.2125 | 0.1704 | OK |
| hybrid | 0.9530 | 0.9775 | 0.9925 | 0.9727 | OK |
| hybrid_rerank | 0.9539 | 0.9756 | 1.0000 | 0.9786 | OK |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.9744 | 0.9765 | 0.9799 |
| fts_only | 0.1699 | 0.1704 | 0.1704 |
| hybrid | 0.9703 | 0.9727 | 0.9760 |
| hybrid_rerank | 0.9764 | 0.9786 | 0.9852 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 0 | 0 | 0 |
| fts_only | 156 | 0 | 156 |
| hybrid | 0 | 0 | 0 |
| hybrid_rerank | 0 | 0 | 0 |

## track-b：长文档检索（Wikipedia 文章聚合，覆盖分块+父子+检索全链路）

语料 709 份 / query 200 条。

| 通道组合 | recall@5 | recall@10 | mrr@10 | ndcg@10 | 状态 |
|---|---|---|---|---|---|
| vector_only | 0.7605 | 0.7866 | 0.9140 | 0.7859 | OK |
| fts_only | 0.1644 | 0.1727 | 0.2333 | 0.1798 | OK |
| hybrid | 0.7634 | 0.7876 | 0.8899 | 0.7742 | OK |
| hybrid_rerank | 0.7710 | 0.7930 | 0.9098 | 0.7932 | OK |

阈值敏感性（ndcg@10）:

| 通道组合 | @0.50 | @0.60 | @0.80 |
|---|---|---|---|
| vector_only | 0.7779 | 0.7859 | 0.8005 |
| fts_only | 0.1803 | 0.1798 | 0.1804 |
| hybrid | 0.7683 | 0.7742 | 0.7875 |
| hybrid_rerank | 0.7878 | 0.7932 | 0.8032 |

未命中归因（主阈值）:

| 通道组合 | 未命中 | gold 文档已召回（分块/匹配损耗） | gold 文档未召回 |
|---|---|---|---|
| vector_only | 6 | 5 | 1 |
| fts_only | 150 | 4 | 146 |
| hybrid | 6 | 5 | 1 |
| hybrid_rerank | 6 | 5 | 1 |

## 如何解读

- **先看 track-a 的 hybrid vs vector_only / fts_only**：混合检索的增益是否成立，是琅嬛核心架构假设的直接验证；hybrid 的 recall@10 应不低于两路单用的较大者。
- **track-a 与 track-b 的差距**衡量分块全链路的损耗：同一批 query 与 gold，track-b 变差越多，说明分块/父子聚合对召回的伤害越大，是调整 chunker 参数的主要依据。
- **hybrid_rerank 与 hybrid 的差值**衡量重排增益；未配置 rerank 模型时该行为 N/A。
- **对比两次 run**：`diff` 两份 metrics.json，指纹完全相同（数据集 manifest、分块、embedding、矩阵）时指标差异才可归因于代码/参数变化；同指纹重复 run 指标应逐位一致。
- **阈值敏感性**：不同阈值的 ndcg 差异过大时，说明命中判定偏松/偏严，先校准阈值再下结论。

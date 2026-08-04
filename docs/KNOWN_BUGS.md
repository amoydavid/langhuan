# 已知缺陷与技术债（KNOWN_BUGS）

本文件记录已识别、但尚未修复的缺陷与技术债。每条包含背景、影响、现状与建议方向，供后续排期处理。修复后请删除对应条目并在提交说明中引用。

---

## KB-001：异步解析资产归档非原子，重试可产生孤儿存储对象

**位置：** `internal/application/pipeline/document_pipeline.go` `CompleteAsyncParse`

**引入版本：** v0.7.0（MinerU 异步解析 + 资产归档链路）

**背景：**

`CompleteAsyncParse` 把异步解析结果落库时分三步独立操作，且不共享事务：

1. `AssetResolver.ResolveWithCandidates` —— 上传图片到对象存储（外部副作用，不可回滚）+ 重写 markdown
2. `assets.DeleteAssetsByRevision` + `assets.CreateAssets` —— 清旧资产再写新资产（两次 DB 写，各自独立）
3. `revisions.CompleteParse` —— 更新 revision 为 ready（自带 `WithinWorkspace` 事务，但只锁 revision 行）

三者跨三个独立 DB 操作，且第 1 步的 `store.Put` 是不可回滚的 OSS/local 写入。

**触发条件：**

- `CreateAssets` 成功、`CompleteParse` 失败（DB 错误、worker 崩溃、context 取消）
- 或资产写入与 revision 完成之间发生进程中断

**影响：**

- **数据正确性：不受影响。** 重试时 `CompleteAsyncParse` 幂等重入：`DeleteAssetsByRevision` 先清旧、`ResolveWithCandidates` 重新归档、`CreateAssets` 重写，revision 最终一致。
- **唯一损失：孤儿存储对象。** 重试时旧 asset 对应的 storage key 被删除（DB 行），但其指向的 OSS/local 文件未被清理，成为存储垃圾。重试 N 次最多产生 N 套孤儿对象。
- **warning 不会重复累加：** `manifest` 每次 Poll 重新构造（全新对象），`CompleteAsyncParse` 里 `append(manifest.Warnings, ...)` 不读取 DB 已有值，故重试不会让 warning 翻倍。

**现状评估：** 非阻塞。单文档资产量有限（受 `MaxCountPerDocument` 截断），孤儿对象可通过定期 GC 回收。不影响检索正确性与用户体验。

**建议修复方向：**

- 把 `DeleteAssetsByRevision` + `CreateAssets` 纳入 `CompleteParse` 的同一事务边界（`WithinWorkspace` tx）。
- 具体做法：让 `AssetRepository` 提供接受 tx 的方法（如 `CreateAssetsTx(ctx, tx, assets)`），或在 revision repository 的事务回调内执行资产写入。
- 难点：`AssetResolver.store.Put`（外部存储上传）天然在事务外，需保证「先完成全部上传、再在事务内统一落 DB 行」的顺序，避免事务回滚后 storage 对象已写。

**相关代码：**

```
internal/application/pipeline/document_pipeline.go:124  CompleteAsyncParse
internal/infrastructure/db/document_asset_repository.go:35  CreateAssets
internal/infrastructure/db/document_revision_repository.go:42  CompleteParse
```

---

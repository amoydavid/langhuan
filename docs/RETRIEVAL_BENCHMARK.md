
## 5. 在线口径注记（v1.4.0）

离线评测（`make eval`）回答"检索算法好不好"；v1.4.0 的健康面板把同一口径带到了在线：空召回率 = `empty/(available+empty+degraded)`（failed 剔除分母、排除管理台检索测试流量 `transport='console'`）。两者的关系与边界：

- 离线 qrels 是静态数据集上的 oracle；在线空召回率是真实 query 分布下的信号——评测框架遗留的"下一触发=真实 query 遥测"（§4.7）由本能力承接。
- 在线空召回率**不能**替代回归评测：它的分母随使用量波动、无 gold 标注、受内容更新影响。修改 chunker/RRF/embedding 的 PR 仍须附 `make eval` 新旧 metrics.json 对比（§回归约定不变）。
- 沉睡内容清单的样本护栏（完成 run<10 不出清单）与评测的"无标注不算数"是同一原则：样本不足时宁可不说话。

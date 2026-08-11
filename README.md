<div align="center">

# 琅嬛 Langhuan

**简体中文** | [English](README.en.md)

**把知识库变成可被 MCP 调用的检索服务 —— 单二进制、中文友好的 RAG 知识处理层**

*Turn your knowledge base into an MCP-callable retrieval service — a single-binary, Chinese-friendly knowledge processing layer for RAG.*

[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-blue.svg)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17%2B-4169E1.svg)](https://www.postgresql.org/)
[![MCP](https://img.shields.io/badge/MCP-over%20HTTP-000000.svg)](https://modelcontextprotocol.io/)
[![Website](https://img.shields.io/badge/website-langhuan.dev-FF6B35.svg)](https://langhuan.dev)

🌐 **官网 Website**: [https://langhuan.dev](https://langhuan.dev)

</div>

---

## 这是什么？ What is Langhuan?

琅嬛是一个**知识转化与检索服务**，位于 RAG 工程中的知识处理层。它把 `pdf / docx / markdown / txt / csv / xlsx` 等文档转成可检索、可向量化、可追溯的结构，并通过 **REST** 与 **MCP over HTTP** 对外提供导入、检索与删除能力。PDF 解析基于 [MinerU Cloud](https://mineru.net)，开箱即用。

**琅嬛不做的事**：不生成 LLM 答案、不编排 Chat/Agent。它专注于把「文档 → 检索服务」这一段做到生产级——这让它可以作为任何 LLM 应用、MCP 客户端或 Agent 的知识底座，而不是又一个大而全的平台。

> Langhuan is a **knowledge processing layer** for RAG. It turns documents (`pdf / docx / markdown / txt / csv / xlsx`) into retrievable, vectorizable, traceable structures and serves them over REST and **MCP over HTTP**. PDF parsing is powered by MinerU Cloud. It does **not** generate LLM answers or orchestrate chat — it is the knowledge foundation your LLM app, MCP client, or agent calls into.

## 特性 Highlights

- **🀄 中文原生的混合检索** — pgvector 向量 + PostgreSQL FTS 全文 + **zhparser 中文分词** + 确定性 RRF 融合。自托管 RAG 中稀缺的「中文关键词检索」能力，开箱即用。
  *Hybrid retrieval with native Chinese tokenization: pgvector + PostgreSQL FTS (zhparser) + deterministic RRF fusion.*
- **🔌 MCP over HTTP 原生支持** — `knowledge_search`、`document_ingest` 等工具直接暴露给 MCP 客户端（Claude 等），知识库即插即用。
  *First-class MCP over HTTP — expose your knowledge base as MCP tools out of the box.*
- **📦 单二进制全栈** — REST + MCP + worker + Web Console 内嵌于一个二进制（`go:embed`），无 Node 运行时、无微服务。
  *Single binary: REST + MCP + async worker + embedded Web Console. Zero runtime dependencies.*
- **🧭 全程可追溯** — 每个 chunk 都能回到源文档、版本与页码/行列/偏移锚点；导入、分块、索引全链路幂等。每次检索生成稳定 `search_id` 与可校验 citation，管理员可在保留期内按原 query 回放。
  *Every chunk traces back to source document, revision and anchor (page/row/offset). Idempotent async pipeline. Each retrieval yields a stable `search_id` with verifiable citations, replayable by admins within the retention window.*
- **🏢 多租户** — Workspace 即租户边界，成员角色 + Workspace API Key 细粒度鉴权。
  *Workspace-scoped isolation with role-based access and scoped API keys.*

## 架构 Architecture

```
File / Web / FAQ 导入
  -> 稳定 Document 身份 + 不可变 Revision
  -> asynq 异步任务链：解析 → 资产归档 → 分块 → 索引
  -> RetrievalEntry 同行保存 halfvec 向量 + FTS tsvector + 返回内容
  -> 原子发布到唯一 active Generation
  -> Vector + FTS + deterministic RRF 融合检索
  -> REST /api/v1/*  +  MCP /mcp
```

技术基线：Go 1.26 · Gin · GORM + PostgreSQL 17 + pgvector（halfvec / HNSW）· PostgreSQL FTS（zhparser）· asynq + Redis · 对象存储（OSS / Local）。

## 快速开始 Quick Start

需要 Docker。一条命令启动完整服务（PostgreSQL + pgvector + zhparser、Redis、Langhuan 本体）：

```bash
docker compose up -d --build
```

打开 **[http://localhost:8080](http://localhost:8080)** ，完成首次初始化（创建管理员账号）→ 创建工作区 → 创建知识库 → 导入文档 → 在检索页验证中英文混合检索。

> Requires only Docker. One command brings up the full stack: `docker compose up -d --build`, then open http://localhost:8080.

### 手动安装 Manual Install

- [Ubuntu 24 / macOS 安装 PostgreSQL + pgvector + zhparser](docs/DATABASE_GUIDELINES.md#72-手动安装-pgvector--zhparserubuntu-24--macos)
- [架构与设计](docs/ARCHITECTURE.md) · [数据库开发指南](docs/DATABASE_GUIDELINES.md) · [路线图](ROADMAP.md)

## 与 RAG 平台的定位差异 Positioning

琅嬛不是 Dify/RAGFlow 的替代品，而是它们的**下层**：它把「知识处理与检索」这一段做到极致，再通过 MCP 供上层应用调用。

| | 琅嬛 Langhuan | Dify / RAGFlow 等平台 |
|---|---|---|
| 定位 | 知识处理层（无 LLM 编排） | 应用平台（含 Chat/Agent） |
| 中文关键词检索 | ✅ zhparser + RRF 原生 | 依赖向量，FTS 较弱 |
| 交付 | 单二进制 + 标准 PG | 多容器全家桶 |
| MCP | 原生 MCP over HTTP | 需额外桥接 |
| 可追溯 | chunk → 锚点全链路 | 部分 |

## 路线图 Roadmap

v0.7.0 ~ v0.9.0 已全部完成：PDF 解析（MinerU Cloud）、可靠性与可观测性（重试 / reindex / OTel）、检索证据血缘与可回放检索。

下一里程碑：**v1.0.0 首次对外发布** —— 冻结 REST / MCP / 认证 / 错误码兼容基线，完善安装、运维与安全文档。详见 [ROADMAP.md](ROADMAP.md)。

## 贡献 Contributing

欢迎 Issue 与 PR。开发指南见 [AGENTS.md](AGENTS.md)（面向贡献者与 AI Agent）。提交请遵循 Conventional Commits，主题与内容以中文为主。

## 作者 Author

**Liu Wei** · [https://liuw.net](https://liuw.net), 你也可以叫我 **amoydavid**

## License

[MIT](LICENSE) © 2026 Langhuan Contributors

---

*琅嬛 —— 书藏琅嬛，取之有道。*

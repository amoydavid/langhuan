<div align="center">

# 琅嬛 Langhuan

**Turn your knowledge base into an MCP-callable retrieval service — a single-binary, Chinese-friendly knowledge processing layer for RAG.**

[简体中文](README.md) | **English**

[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-blue.svg)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17%2B-4169E1.svg)](https://www.postgresql.org/)
[![MCP](https://img.shields.io/badge/MCP-over%20HTTP-000000.svg)](https://modelcontextprotocol.io/)
[![Website](https://img.shields.io/badge/website-langhuan.dev-FF6B35.svg)](https://langhuan.dev)

🌐 **Website**: [https://langhuan.dev](https://langhuan.dev)

</div>

---

## What is Langhuan?

Langhuan is a **knowledge processing and retrieval service** that sits in the knowledge layer of a RAG pipeline. It turns documents (`pdf / docx / markdown / txt / csv / xlsx`) into retrievable, vectorizable, and traceable structures, and exposes ingestion, retrieval, and deletion over **REST** and **MCP over HTTP**. PDF parsing is powered by [MinerU Cloud](https://mineru.net) — works out of the box.

**What Langhuan does *not* do**: it does not generate LLM answers or orchestrate chat/agents. It focuses on doing the "documents → retrieval service" segment to production grade — so it can serve as the knowledge foundation for any LLM application, MCP client, or agent, rather than being yet another all-in-one platform.

## Highlights

- **🀄 Native Chinese hybrid retrieval** — pgvector vectors + PostgreSQL FTS + **zhparser Chinese tokenization** + deterministic RRF fusion. The "Chinese keyword search" capability that is rare among self-hosted RAG stacks, out of the box.
- **🔌 First-class MCP over HTTP** — `knowledge_search`, `document_ingest` and other tools are exposed directly to MCP clients (Claude, etc.); your knowledge base is plug-and-play.
- **📦 Single binary, full stack** — REST + MCP + async worker + embedded Web Console in one binary (`go:embed`). No Node runtime, no microservices.
- **🧭 Fully traceable** — every chunk traces back to its source document, revision, and anchor (page / row / offset). The ingestion → chunking → indexing pipeline is idempotent end-to-end. Each retrieval yields a stable `search_id` with verifiable citations, replayable by admins within the retention window.
- **🏢 Multi-tenant** — Workspace as the tenant boundary, with member roles and Workspace API Keys for fine-grained auth.

## Architecture

```
File / Web / FAQ ingestion
  -> stable Document identity + immutable Revisions
  -> asynq async task chain: parse → asset archival → chunk → index
  -> RetrievalEntry stores halfvec vector + FTS tsvector + returned content in the same row
  -> atomic publish to a single active Generation
  -> Vector + FTS + deterministic RRF fusion retrieval
  -> REST /api/v1/*  +  MCP /mcp
```

Tech baseline: Go 1.26 · Gin · GORM + PostgreSQL 17 + pgvector (halfvec / HNSW) · PostgreSQL FTS (zhparser) · asynq + Redis · object storage (OSS / Local). Since v1.0.0: SQLite zero-config standalone mode (modernc.org/sqlite + sqlite-vec + FTS5 + gse Chinese tokenizer).

## Quick Start

### Zero-Config (Recommended)

No Docker, PostgreSQL, or Redis needed—just run the binary:

```bash
make standalone   # or: go run ./cmd/langhuan, or just ./langhuan
```

On first run, Langhuan auto-provisions a SQLite database, encryption key, and config under `~/.langhuan-data/`. Open [http://127.0.0.1:8080](http://127.0.0.1:8080) → register admin → create workspace → create knowledge base → ingest documents → search.

> No Docker, PostgreSQL, or Redis needed. Just run the binary—Langhuan auto-provisions everything under `~/.langhuan-data/`.

**Standalone limitations**: in-process memory queue (tasks lost on crash; recovered by source cleanup + user retry); recommended for < tens of thousands of embeddings (exact brute-force vector scan, 100% recall); full retrieval features (vector + Chinese FTS + RRF + rerank). For high-concurrency production, use the PostgreSQL + Redis deployment below.

### Production Deployment (PostgreSQL + Redis)

Requires Docker. One command brings up the full stack:

```bash
docker compose up -d --build
```

Open [http://localhost:8080](http://localhost:8080), finish first-run setup → create workspace → create knowledge base → ingest documents → search.

> Full production stack with one command: `docker compose up -d --build`.

### Manual Install

- [Install PostgreSQL + pgvector + zhparser on Ubuntu 24 / macOS](docs/DATABASE_GUIDELINES.md#72-手动安装-pgvector--zhparserubuntu-24--macos) (Chinese)
- [Architecture & Design](docs/ARCHITECTURE.md) · [Database Development Guide](docs/DATABASE_GUIDELINES.md) (Chinese) · [Backup & Restore](docs/operations/backup-restore.md) · [Roadmap](ROADMAP.md)

## Positioning vs. RAG Platforms

Langhuan is not a replacement for Dify/RAGFlow — it is the **layer below** them. It takes the "knowledge processing and retrieval" segment to the extreme, then exposes it to upper-layer applications via MCP.

| | Langhuan | Dify / RAGFlow et al. |
|---|---|---|
| Positioning | Knowledge processing layer (no LLM orchestration) | Application platform (with Chat/Agent) |
| Chinese keyword search | ✅ native zhparser + RRF | Vector-heavy, weaker FTS |
| Delivery | Single binary + standard PG | Multi-container bundle |
| MCP | Native MCP over HTTP | Needs extra bridging |
| Traceability | chunk → anchor, full chain | Partial |

## Roadmap

v0.7.0 – v0.9.0 are all complete: PDF parsing (MinerU Cloud), reliability & observability (retry / reindex / OTel), and search evidence lineage with replayable retrieval.

Next milestone: **v1.0.0 first public release** — freeze the REST / MCP / auth / error-code compatibility baseline, and finish install, ops, and security docs. See [ROADMAP.md](ROADMAP.md).

## Contributing

Issues and PRs are welcome. See [AGENTS.md](AGENTS.md) for the development guide (aimed at contributors and AI agents). Please follow Conventional Commits.

## Author

**Liu Wei** · [https://liuw.net](https://liuw.net), or you can call my David (amoydavid).

## License

[MIT](LICENSE) © 2026 Langhuan Contributors

---

*琅嬛 (Láng huán) — from the mythic "Library of Láng huán": a storehouse of all knowledge, where everything has its proper place and can be retrieved the right way.*

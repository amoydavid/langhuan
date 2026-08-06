# Web SPA Embed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make linux` rebuild `web/dist` and embed that Vite SPA into the Linux `langhuan` binary while ordinary Go development remains independent of frontend build artifacts.

**Architecture:** A build-tagged `webspa` package exposes either an embedded `dist` filesystem (`web_embed`) or `nil` (ordinary builds). The command injects this optional `fs.FS` into the Gin router, whose focused SPA handler serves real assets and falls back to `index.html` without crossing REST, MCP, or removed legacy REST boundaries.

**Tech Stack:** Go 1.26, `embed`, `io/fs`, `net/http`, Gin, Vite 8, pnpm, GNU Make.

## Global Constraints

- Follow Red → Green → Refactor: every runtime behavior begins with a failing test and the failure must be observed.
- Keep `/api/v1`, `/mcp`, `/healthz`, `/auth/*`, and `/admin/*` outside SPA fallback; `/invitations/:token` belongs to the TanStack Router SPA.
- Do not commit `web/dist`; ordinary `go test ./...` must compile without it.
- Only `make linux` adds `-tags web_embed`; development commands retain the current Vite split.
- Do not add runtime static-directory configuration or new external dependencies.

---

### Task 1: Injectable SPA HTTP Handler

**Files:**
- Create: `internal/interfaces/http/spa_test.go`
- Create: `internal/interfaces/http/spa.go`
- Modify: `internal/interfaces/http/router.go`

**Interfaces:**
- Consumes: `fs.FS`, Gin `*gin.Context`, and existing `Dependencies` / `NewRouter`.
- Produces: `Dependencies.SPA fs.FS` and `newSPAHandler(spaFS fs.FS) gin.HandlerFunc`.

- [ ] **Step 1: Write the failing handler tests**

Create `spa_test.go` with an `fstest.MapFS` containing `index.html` and `assets/app.js`. Through `NewRouter(Dependencies{SPA: bundle})`, cover:

```go
tests := []struct {
    name, method, target string
    wantStatus           int
    wantBody             string
    wantContentType      string
}{
    {"root", http.MethodGet, "/", http.StatusOK, "<html>console</html>", "text/html"},
    {"asset", http.MethodGet, "/assets/app.js", http.StatusOK, "console.log('langhuan')", "text/javascript"},
    {"deep route", http.MethodGet, "/workspaces/demo/kb/example", http.StatusOK, "<html>console</html>", "text/html"},
    {"head", http.MethodHead, "/assets/app.js", http.StatusOK, "", "text/javascript"},
    {"write method", http.MethodPost, "/workspaces/demo", http.StatusNotFound, "", ""},
    {"traversal", http.MethodGet, "/../secret", http.StatusNotFound, "", ""},
}
```

Add a second table test proving `/api/v1/unknown` remains JSON 404 and `/mcp`, `/healthz`, `/auth/me`, and `/admin/users/id/password-reset` never return the SPA body. The serving table must prove `/invitations/token` falls back to the SPA because that is a real TanStack Router route.

- [ ] **Step 2: Run the tests and observe RED**

Run:

```bash
go test ./internal/interfaces/http -run 'TestEmbeddedSPA' -count=1
```

Expected: compilation fails because `Dependencies` has no `SPA` field.

- [ ] **Step 3: Add the minimal handler and router wiring**

Add `SPA fs.FS` to `Dependencies`. Implement `newSPAHandler` in `spa.go` using `fs.Stat` and `http.ServeFileFS`:

```go
func newSPAHandler(spaFS fs.FS) gin.HandlerFunc {
    if spaFS == nil {
        return nil
    }
    return func(c *gin.Context) {
        if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
            c.Status(http.StatusNotFound)
            return
        }
        name, ok := validSPAPath(c.Request.URL.Path)
        if !ok {
            c.Status(http.StatusNotFound)
            return
        }
        info, err := fs.Stat(spaFS, name)
        if err == nil && !info.IsDir() {
            http.ServeFileFS(c.Writer, c.Request, spaFS, name)
            return
        }
        if err != nil && !errors.Is(err, fs.ErrNotExist) {
            c.Status(http.StatusInternalServerError)
            return
        }
        if _, err := fs.Stat(spaFS, "index.html"); err != nil {
            c.Status(http.StatusInternalServerError)
            return
        }
        http.ServeFileFS(c.Writer, c.Request, spaFS, "index.html")
    }
}
```

`validSPAPath` rejects non-absolute paths, dot segments, backslashes, repeated separators, and names rejected by `fs.ValidPath`; `/` maps to `index.html`. In `NewRouter`, update `NoRoute` in this order:

1. `/api/v1` prefix → existing JSON 404.
2. `/mcp`, `/mcp/*`, and removed root REST prefixes → plain 404.
3. Non-nil SPA handler → serve SPA.
4. Otherwise → plain 404.

- [ ] **Step 4: Run focused and package tests and observe GREEN**

```bash
go test ./internal/interfaces/http -run 'TestEmbeddedSPA|TestUnknownAPIRoute|TestLegacyRoot' -count=1
go test ./internal/interfaces/http -count=1
```

Expected: both pass.

- [ ] **Step 5: Refactor and keep tests green**

Keep namespace predicates in `spa.go`; run `gofmt` and rerun the package tests.

---

### Task 2: Build-Tagged Web Bundle

**Files:**
- Create: `web/embed_test.go`
- Create: `web/embed.go`
- Create: `web/embed_dev.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Consumes: generated `web/dist`, `fs.Sub`, and `langhttp.Dependencies.SPA`.
- Produces: `webspa.SPA fs.FS`, non-nil only under `web_embed`.

- [ ] **Step 1: Write the failing embedded bundle test**

Create `embed_test.go` with `//go:build web_embed`. Assert `SPA` contains non-directory `index.html`, an `assets` directory, and at least one asset file via `fs.WalkDir`.

- [ ] **Step 2: Build the frontend and observe RED**

```bash
pnpm --dir web build
go test -tags web_embed ./web -count=1
```

Expected: frontend build passes, then Go compilation fails because `SPA` is undefined.

- [ ] **Step 3: Implement production and development variants**

Create `embed.go`:

```go
//go:build web_embed

package webspa

import (
    "embed"
    "io/fs"
)

//go:embed all:dist
var embedded embed.FS

// SPA contains the production Vite bundle rooted at dist.
var SPA = mustSub(embedded, "dist")

func mustSub(root fs.FS, dir string) fs.FS {
    sub, err := fs.Sub(root, dir)
    if err != nil {
        panic("加载内嵌 Web SPA 失败: " + err.Error())
    }
    return sub
}
```

Create `embed_dev.go` with `//go:build !web_embed`, import `io/fs`, and declare a documented `var SPA fs.FS`. Import `github.com/dajee/langhuan/web` as `webspa` in `cmd/langhuan/main.go` and pass `SPA: webspa.SPA`.

- [ ] **Step 4: Run tagged, command, and ordinary tests and observe GREEN**

```bash
gofmt -w web/embed.go web/embed_dev.go web/embed_test.go cmd/langhuan/main.go
go test -tags web_embed ./web -count=1
go test ./cmd/langhuan ./web -count=1
```

Expected: all pass.

---

### Task 3: Linux Build Pipeline and Complete Verification

**Files:**
- Modify: `Makefile`

**Interfaces:**
- Consumes: `pnpm --dir web build`, `web_embed`, and `./cmd/langhuan`.
- Produces: `bin/langhuan-linux-amd64` containing the current Vite bundle.

- [ ] **Step 1: Add the build dependency**

```make
.PHONY: dev web linux _web-build

_web-build:
	pnpm --dir web build

linux: _web-build
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags web_embed -o bin/langhuan-linux-amd64 ./cmd/langhuan
```

- [ ] **Step 2: Prove the composed build works**

```bash
make linux
test -x bin/langhuan-linux-amd64
rg -a -m 1 -o '琅嬛管理台' bin/langhuan-linux-amd64
```

Expected: frontend build precedes Go build, the binary exists, and contains the SPA title.

- [ ] **Step 3: Run the complete verification matrix**

```bash
go test ./... -count=1
go vet ./...
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
go test -tags web_embed ./web ./internal/interfaces/http ./cmd/langhuan -count=1
make linux
git diff --check
```

Expected: every command exits 0 with no test, vet, Biome, TypeScript, or whitespace errors.

- [ ] **Step 4: Review the final diff**

Run `git status --short`, `git diff --stat`, and `git diff`. Confirm only the plan, Makefile, embed files, SPA handler/tests, router dependency, and command wiring changed; confirm `web/dist` and `bin/` remain ignored.

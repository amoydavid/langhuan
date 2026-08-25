.PHONY: dev standalone web test-image test-integration test-sqlite linux _web-build eval eval-prepare eval-smoke

# 测试专用 PostgreSQL 镜像（pgvector + zhparser，见 docker/postgres-test/Dockerfile）
TEST_PG_IMAGE ?= langhuan-test-postgres:pg17

dev:
	go run ./cmd/langhuan -config ./config.local.yaml

standalone:
	go run ./cmd/langhuan

web:
	cd web && VITE_DEV_PROXY_TARGET=http://127.0.0.1:8090 pnpm dev

test-image:
	docker build -t $(TEST_PG_IMAGE) -f docker/postgres-test/Dockerfile .

test-integration: test-image
	@set -eu; \
	container_name="langhuan-test-$$(date +%s)-$$$$"; \
	cleanup() { docker rm -f "$$container_name" >/dev/null 2>&1 || true; }; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	docker run --rm -d \
		--name "$$container_name" \
		-e POSTGRES_DB=langhuan_test \
		-e POSTGRES_USER=langhuan \
		-e POSTGRES_PASSWORD=langhuan \
		-p 127.0.0.1::5432 \
		$(TEST_PG_IMAGE) >/dev/null; \
	attempt=0; \
	until docker exec "$$container_name" pg_isready -U langhuan -d langhuan_test >/dev/null 2>&1; do \
		attempt=$$((attempt + 1)); \
		if [ "$$attempt" -ge 60 ]; then \
			echo "等待测试 PostgreSQL 就绪超时" >&2; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	port=$$(docker port "$$container_name" 5432/tcp | awk -F: 'NR == 1 { print $$NF }'); \
	LANGHUAN_TEST_DATABASE_DSN="postgres://langhuan:langhuan@127.0.0.1:$$port/langhuan_test?sslmode=disable" \
	LANGHUAN_TEST_RUN_ID="$$container_name" \
	go test -tags=integration ./... -count=1

# SQLite 相关单元测试（不依赖 Docker；db/migrate 的 SQLite 集成测试由 test-integration 覆盖）
test-sqlite:
	go test ./internal/infrastructure/config/... ./internal/infrastructure/datadir/... ./internal/adapters/auth/... ./internal/adapters/queue/memory/... ./internal/adapters/tokenizer/... -count=1

# 离线检索评测（langhuan-eval，见 docs/superpowers/specs/2026-08-24-retrieval-eval-design.md）
# eval：prepare（数据集缺失时自动下载，首次约 730MB，走 HF 镜像）+ run（standalone 拉起被测系统）
eval:
	@test -f eval.config.yaml || { echo "缺少 eval.config.yaml（cp eval.config.example.yaml eval.config.yaml 后按需修改）" >&2; exit 1; }
	@test -f .eval-data/miracl-zh/manifest.json || $(MAKE) eval-prepare
	go run ./cmd/langhuan-eval run -config eval.config.yaml

eval-prepare:
	go run ./cmd/langhuan-eval prepare

# 离线冒烟：本地确定性 mock embedding + 入库微型数据集（无 HF/网络依赖），验证评测全链路（指标无语义意义）
eval-smoke:
	@set -eu; \
	port=19829; \
	go run ./cmd/langhuan-eval mock-embedding -addr 127.0.0.1:$$port & \
	mock_pid=$$!; \
	trap 'kill $$mock_pid 2>/dev/null || true' EXIT; \
	sleep 1; \
	go run ./cmd/langhuan-eval run -config eval.config.smoke.yaml

_web-build:
	pnpm --dir web build

linux: _web-build
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags web_embed -trimpath -ldflags="-s -w" -o bin/langhuan-linux-amd64 ./cmd/langhuan

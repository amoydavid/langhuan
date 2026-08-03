# syntax=docker/dockerfile:1

# langhuan 应用镜像：单二进制（REST + MCP + worker + 内嵌 Web Console）。
# 构建分三段：Web 静态资源 -> Go web_embed 二进制 -> 精简运行镜像。

# ---- 阶段 1：构建 Web Console 静态资源 ----
FROM node:22-alpine AS web-builder
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- 阶段 2：构建 langhuan 二进制（内嵌 web/dist）----
FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -tags web_embed -trimpath -o /out/langhuan ./cmd/langhuan

# ---- 阶段 3：运行 ----
FROM debian:bookworm-slim
# Go 静态二进制通过系统 CA 校验证书（MinerU/embedding 等 HTTPS 调用需要）
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go-builder /out/langhuan /usr/local/bin/langhuan
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["langhuan"]

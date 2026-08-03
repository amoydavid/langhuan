# 测试专用 PostgreSQL 镜像：pgvector + zhparser（中文分词）
#
# 背景（AGENTS.md 5.10 测试数据库隔离铁律）：自动化测试的 PostgreSQL 只能来自
# 测试期间临时拉起的 docker 容器。官方 pgvector/pgvector 镜像不带中文分词扩展，
# 而迁移 000011 需要 zhparser，因此在此镜像上预装：
#   - pgvector（基础镜像自带，含 halfvec/HNSW）
#   - SCWS（zhparser 依赖的中文分词库，debian 无现成包，源码编译）
#   - zhparser（PostgreSQL 中文全文检索扩展）
#
# 用法：make test-image（构建出 langhuan-test-postgres:pg17）
# testcontainers（internal/testsupport/postgres.go）与 Makefile test-integration
# 均引用该镜像。
#
# 海外站点加速：构建时若需代理，可传
#   docker build --build-arg HTTP_PROXY=http://your-proxy:port \
#     --build-arg HTTPS_PROXY=http://your-proxy:port \
#     --build-arg ALL_PROXY=socks5://your-proxy:port ...

FROM pgvector/pgvector:pg17

# 固定版本保证可复现
ARG ZH_PARSER_VERSION=v2.3
ARG SCWS_VERSION=1.2.3
# 构建期网络代理（可选）
ARG HTTP_PROXY=""
ARG HTTPS_PROXY=""
ARG ALL_PROXY=""

USER root

# 官方 postgres 镜像的 pg_config 不在默认 PATH，需显式加入
ENV PATH="/usr/lib/postgresql/17/bin:${PATH}"

RUN set -eux; \
    export http_proxy="${HTTP_PROXY}" https_proxy="${HTTPS_PROXY}" all_proxy="${ALL_PROXY}"; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        autoconf \
        automake \
        build-essential \
        ca-certificates \
        curl \
        git \
        libtool \
        postgresql-server-dev-17; \
    \
    # SCWS 1.2.3：源码编译安装到 /usr/local（zhparser 默认 SCWS_HOME=/usr/local）。
    # github 源码包是 autotools 工程，configure 需由 autoreconf 生成。
    curl -fsSL -o /tmp/scws.tar.gz \
        "https://github.com/hightman/scws/archive/refs/tags/${SCWS_VERSION}.tar.gz"; \
    mkdir -p /tmp/scws && tar -xzf /tmp/scws.tar.gz --strip-components=1 -C /tmp/scws; \
    (cd /tmp/scws && autoreconf -i && ./configure --prefix=/usr/local && make && make install); \
    ldconfig; \
    \
    # zhparser：编译安装到 PG 扩展目录
    git clone --depth 1 --branch "${ZH_PARSER_VERSION}" \
        https://github.com/amutu/zhparser.git /tmp/zhparser; \
    make -C /tmp/zhparser; \
    make -C /tmp/zhparser install; \
    \
    rm -rf /tmp/scws /tmp/scws.tar.gz /tmp/zhparser; \
    # 清理编译依赖，保持镜像体积可控
    apt-get purge -y --auto-remove build-essential curl git; \
    rm -rf /var/lib/apt/lists/*

# 恢复官方镜像默认的 postgres 用户
USER postgres

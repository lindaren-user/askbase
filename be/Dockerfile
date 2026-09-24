# ============================================================================
# AskBase 后端镜像：API、文档 worker 与 Outbox relay
# 构建上下文为仓库根目录：docker build -f be/Dockerfile -t askbase .
# ============================================================================

# ---- 后端构建 ----
FROM golang:1.26-bookworm AS be
ARG PDFIUM_VERSION=7809
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates clang curl lld \
    && rm -rf /var/lib/apt/lists/* \
    && case "${TARGETARCH}" in \
         amd64) PDFIUM_ARCH=x64 ;; \
         arm64) PDFIUM_ARCH=arm64 ;; \
         *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
       esac \
    && mkdir -p /opt/pdfium \
    && curl --retry 5 --retry-all-errors -fsSL "https://github.com/kognitos/pdfium-static/releases/download/chromium%2F${PDFIUM_VERSION}/pdfium-linux-${PDFIUM_ARCH}-static.tgz" \
    | tar -xz -C /opt/pdfium
WORKDIR /build/be
COPY be/go.mod be/go.sum ./
RUN go mod download
COPY be/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /out/askbase-server ./cmd/server \
    && CGO_ENABLED=1 GOOS=linux \
    CGO_LDFLAGS="/opt/pdfium/lib/libpdfium.a /opt/pdfium/lib/libc++.a /opt/pdfium/lib/libc++abi.a -fuse-ld=lld -Wl,--allow-multiple-definition" \
    go build -ldflags "-s -w" -o /out/askbase-worker ./cmd/worker \
    && CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /out/askbase-relay ./cmd/relay \
    && CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /out/askbase-migrate ./cmd/migrate

# ---- 运行时 ----
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 askbase \
    && useradd --system --uid 10001 --gid askbase --home-dir /app --shell /usr/sbin/nologin askbase
WORKDIR /app
COPY --from=be /out/askbase-server /app/askbase-server
COPY --from=be /out/askbase-worker /app/askbase-worker
COPY --from=be /out/askbase-relay /app/askbase-relay
COPY --from=be /out/askbase-migrate /app/askbase-migrate

# 镜像不内置业务配置；Compose 或部署平台在运行时挂载 config.yaml。
ENV HTTP_HOST=0.0.0.0 \
    HTTP_PORT=8082

EXPOSE 8082
USER 10001:10001
CMD ["/app/askbase-server"]

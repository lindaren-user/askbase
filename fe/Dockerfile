# syntax=docker/dockerfile:1

# ---- 前端构建 ----
FROM node:22-alpine AS build
WORKDIR /build/fe

COPY fe/package.json fe/pnpm-lock.yaml ./
RUN corepack enable \
    && pnpm install --frozen-lockfile

COPY fe/ ./
RUN pnpm build

# ---- 静态资源服务 ----
FROM nginx:1.28-alpine
COPY fe/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /build/fe/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1/healthz || exit 1

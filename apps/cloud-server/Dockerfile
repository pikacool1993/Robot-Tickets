FROM node:24-alpine AS web-builder

WORKDIR /web
RUN corepack enable && corepack prepare pnpm@11.25.0 --activate
COPY admin-web/package.json admin-web/pnpm-lock.yaml admin-web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY admin-web/ ./
RUN pnpm build
RUN cp public/runtime-config.example.js dist/runtime-config.js

FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cloud-server ./cmd/server

FROM debian:bookworm-slim

RUN useradd --system --uid 10001 --no-create-home appuser
WORKDIR /app
COPY --from=builder /out/cloud-server /app/cloud-server
COPY migrations /app/migrations
COPY --from=web-builder /web/dist /app/admin-web/dist
ENV ADMIN_WEB_DIR=/app/admin-web/dist
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/cloud-server"]

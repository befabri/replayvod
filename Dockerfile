# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS dashboard-builder

WORKDIR /dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci

COPY dashboard/ ./

ARG VITE_API_URL=""
ENV VITE_API_URL=${VITE_API_URL}
RUN npm run build && \
    mv dist/client/_shell.html dist/client/index.html

FROM golang:1.26-alpine AS go-builder

WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /replayvod ./cmd/server

FROM alpine:3.21

WORKDIR /app

RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates ffmpeg tzdata

COPY --from=go-builder /replayvod /app/replayvod
COPY --from=dashboard-builder /dashboard/dist/client /app/dashboard
COPY server/config.toml /app/config.toml

RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/data/videos /app/data/thumbnails /app/logs && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

VOLUME ["/app/data", "/app/logs"]

CMD ["/app/replayvod"]

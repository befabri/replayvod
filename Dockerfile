# syntax=docker/dockerfile:1.7

# The dashboard compiles to architecture-independent static assets, so build it
# once on the native build platform regardless of the target arch.
FROM --platform=$BUILDPLATFORM node:24-alpine AS dashboard-builder

WORKDIR /dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci

COPY dashboard/ ./

ARG VITE_API_URL=""
ENV VITE_API_URL=${VITE_API_URL}
RUN npm run build && \
    mv dist/client/_shell.html dist/client/index.html

# Build the Go binary on the native build platform and cross-compile to the
# target arch. CGO is disabled (pure-Go deps), so this is just a GOARCH switch
# and stays fast even when targeting arm64 from an amd64 host.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go-builder

WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ ./
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /replayvod ./cmd/server

FROM alpine:3.23

WORKDIR /app

# hadolint ignore=DL3018
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

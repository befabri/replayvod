# Stage 1: Build dashboard
FROM node:22-alpine AS dashboard-builder

WORKDIR /dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ ./

ENV VITE_API_URL=""
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.26-alpine AS go-builder

WORKDIR /app
COPY server/ ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /replayvod ./cmd/server

# Stage 3: Final image
FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg yt-dlp

COPY --from=go-builder /replayvod /app/replayvod
COPY --from=dashboard-builder /dashboard/dist/client /app/dashboard

RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/data/videos /app/data/thumbnails /app/logs && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

VOLUME ["/app/data", "/app/logs"]

CMD ["/app/replayvod"]

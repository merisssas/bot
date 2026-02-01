FROM golang:1.24.2-alpine AS builder

ARG VERSION="dev"
ARG GitCommit="Unknown"
ARG BuildTime="Unknown"

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 \
    go build -trimpath \
    -ldflags=" \
    -s -w \
    -X 'github.com/merisssas/bot/config.Version=${VERSION}' \
    -X 'github.com/merisssas/bot/config.GitCommit=${GitCommit}' \
    -X 'github.com/merisssas/bot/config.BuildTime=${BuildTime}' \
    -X 'github.com/merisssas/bot/config.Docker=true' \
    " \
    -o saveany-bot .

FROM alpine:3.20

RUN apk add --no-cache curl ffmpeg yt-dlp

WORKDIR /app

COPY --from=builder /app/saveany-bot .
COPY entrypoint.sh .

RUN chmod +x /app/saveany-bot && \
    chmod +x /app/entrypoint.sh

ENTRYPOINT ["/app/entrypoint.sh"]

# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine3.22 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./services/api/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/authenticator ./services/authenticator/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dispatcher ./services/dispatcher/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./services/gateway/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guild ./services/guild/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mailer ./services/mailer/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/media ./services/media/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/message ./services/message/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/presence ./services/presence/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/session ./services/session/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/user ./services/user/v1 && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-authenticator ./services/authenticator/v1/cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-guild ./services/guild/v1/cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-message ./services/message/v1/cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-user ./services/user/v1/cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/grpc-healthcheck ./cmd/grpc-healthcheck

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S cordis && \
    adduser -S -G cordis -h /app cordis

WORKDIR /app
COPY --from=build --chown=cordis:cordis /out/ /app/bin/

USER cordis

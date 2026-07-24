GO ?= go
BUF := $(GO) tool buf
LOCAL_COMPOSE := docker compose --env-file deploy/compose/.env -f deploy/compose/compose.yaml

EXTERNAL_PROTO_FILES := $(shell find proto/api -type f -name '*.proto')
INTERNAL_PROTO_DIRS := proto/authenticator proto/user proto/message proto/guild proto/presence proto/session proto/mailer proto/media
INTERNAL_PROTO_FILES := $(shell find $(INTERNAL_PROTO_DIRS) -type f -name '*.proto')

.PHONY: all generate generate-external generate-internal gen lint test test-integration compose-up compose-down compose-local-config compose-local-up compose-local-down

all: generate

generate: generate-external generate-internal

generate-external: buf.yaml buf.gen.external.yaml $(EXTERNAL_PROTO_FILES)
	$(BUF) generate --template buf.gen.external.yaml

generate-internal: buf.yaml buf.gen.internal.yaml $(INTERNAL_PROTO_FILES)
	$(BUF) generate --template buf.gen.internal.yaml

gen: generate

lint:
	$(BUF) lint

test:
	go test ./...

test-integration:
	go test -tags=integration ./... -count=1 -timeout=10m

compose-up:
	docker compose up -d

compose-down:
	docker compose down

compose-local-config:
	$(LOCAL_COMPOSE) config --quiet

compose-local-up:
	$(LOCAL_COMPOSE) up -d --build

compose-local-down:
	$(LOCAL_COMPOSE) down

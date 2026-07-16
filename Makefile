GO ?= go
BUF := $(GO) tool buf

EXTERNAL_PROTO_FILES := $(shell find proto/api -type f -name '*.proto')
INTERNAL_PROTO_DIRS := proto/authenticator proto/user proto/message proto/guild proto/presence proto/session
INTERNAL_PROTO_FILES := $(shell find $(INTERNAL_PROTO_DIRS) -type f -name '*.proto')

.PHONY: all generate generate-external generate-internal gen lint test

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

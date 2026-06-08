BUF ?= buf

EXTERNAL_PROTO_FILES := $(shell find proto/api -type f -name '*.proto')
INTERNAL_PROTO_DIRS := proto/authenticator proto/user
INTERNAL_PROTO_FILES := $(shell find $(INTERNAL_PROTO_DIRS) -type f -name '*.proto')
GENERATE_TOOLS := $(BUF) protoc-gen-go protoc-gen-connect-go protoc-gen-go-grpc protoc-gen-es

.PHONY: all generate generate-external generate-internal gen check-generate-tools lint test test-integration

all: generate

generate: check-generate-tools generate-external generate-internal

check-generate-tools:
	@for tool in $(GENERATE_TOOLS); do \
		command -v $$tool >/dev/null || { echo "missing required tool: $$tool" >&2; exit 1; }; \
	done

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
	@test -n "$(CORDIS_TEST_POSTGRES_DSN)" || { echo "CORDIS_TEST_POSTGRES_DSN is required" >&2; exit 1; }
	go test -tags=integration ./services/user/v1/internal/store ./services/authenticator/v1/internal/store

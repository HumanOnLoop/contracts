# contracts: HOTL's event and document schemas and the Go library generated from them. See README.md.

# Code generator for the Go types, run with `go run` at a pinned version; it is not a module dependency.
GO_JSONSCHEMA := github.com/atombender/go-jsonschema@v0.24.1
SCHEMA_BASE   := https://github.com/osbe/contracts
GEN_FLAGS     := --only-models --struct-name-from-title --tags json \
	--capitalization ID --capitalization IDs --capitalization URI --capitalization URL --capitalization PO --capitalization SHA256

# The registry and Kafka on the k3d-hotl cluster (platform repo, ADR 0019): the registry by name through Traefik,
# Kafka on its own host port.
REGISTRY_URL := http://hotl-registry.localhost:8480
KAFKA_PORT   := 19092

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.DEFAULT_GOAL := help
.PHONY: help build test lint generate check-generated register check-registry test-kafka

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/^[^:]*:([a-z-]+):.*## /\1\t/' | column -t -s "$$(printf '\t')"

build: ## Compile every package
	go build ./...

test: ## Run the unit tests, including the compatibility check against the last release tag
	go test ./...

lint: ## go vet, and fail if generated code is stale
	go vet ./...
	@$(MAKE) --no-print-directory check-generated

generate: ## Regenerate the Go types and the event catalogue from the schemas
	go run $(GO_JSONSCHEMA) $(GEN_FLAGS) --package events --output events/types_gen.go \
		--schema-package $(SCHEMA_BASE)/documents/intent-package.schema.json=github.com/HumanOnLoop/contracts/documents \
		--schema-package $(SCHEMA_BASE)/documents/review-brief.schema.json=github.com/HumanOnLoop/contracts/documents \
		--schema-package $(SCHEMA_BASE)/documents/rule.schema.json=github.com/HumanOnLoop/contracts/documents \
		--schema-package $(SCHEMA_BASE)/documents/repo-profile.schema.json=github.com/HumanOnLoop/contracts/documents \
		--schema-package $(SCHEMA_BASE)/documents/toolchain-image.schema.json=github.com/HumanOnLoop/contracts/documents \
		--schema-output $(SCHEMA_BASE)/documents/intent-package.schema.json=documents/types_gen.go \
		--schema-output $(SCHEMA_BASE)/documents/review-brief.schema.json=documents/types_gen.go \
		--schema-output $(SCHEMA_BASE)/documents/rule.schema.json=documents/types_gen.go \
		--schema-output $(SCHEMA_BASE)/documents/repo-profile.schema.json=documents/types_gen.go \
		--schema-output $(SCHEMA_BASE)/documents/toolchain-image.schema.json=documents/types_gen.go \
		events/common.schema.json events/*/*.schema.json documents/*.schema.json
	go run ./internal/cmd/gencatalog events/catalog_gen.go

check-generated: generate ## Fail if the generated files differ from the committed ones
	@git diff --exit-code --stat -- events/types_gen.go events/catalog_gen.go documents/types_gen.go || \
		{ echo "generated code is stale: run 'make generate' and commit the result" >&2; exit 1; }

register: ## Register every schema in the k3d-hotl registry, with BACKWARD compatibility and validity rules on each
	go run ./internal/cmd/registry -url $(REGISTRY_URL)/apis/registry/v3 -version $(VERSION) register

check-registry: ## Ask the k3d-hotl registry whether every schema is compatible with its latest registered version
	env HOTL_REGISTRY_URL=$(REGISTRY_URL)/apis/registry/v3 \
		go test ./internal/registry -run TestRegistryAcceptsSchemas -count=1 -v

test-kafka: ## Produce and consume through Kafka on k3d-hotl, on temporary topics
	env HOTL_KAFKA_BROKERS=localhost:$(KAFKA_PORT) go test ./kafka -run Integration -count=1 -v

# Ember — single-binary self-hosted RSS reader
.DEFAULT_GOAL := help

GO            ?= go
NPM           ?= npm
BIN           ?= ./bin/ember
PKG           := ./...
COVER_OUT     ?= coverage.out
COVER_HTML    ?= coverage.html
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS       := -s -w -X main.version=$(VERSION)
WEB_DIR       := web
EMBED_DIR     := internal/web/dist

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-14s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: lint
lint: ## golangci-lint run
	golangci-lint run

.PHONY: test
test: ## go test
	$(GO) test -race -count=1 $(PKG)

.PHONY: cover
cover: ## go test with coverage report
	$(GO) test -race -count=1 -covermode=atomic -coverprofile=$(COVER_OUT) $(PKG)
	$(GO) tool cover -func=$(COVER_OUT) | tail -n 20
	$(GO) tool cover -html=$(COVER_OUT) -o $(COVER_HTML)

.PHONY: web-install
web-install: ## install web deps
	cd $(WEB_DIR) && $(NPM) install

.PHONY: web-build
web-build: ## build the svelte spa to web/dist
	cd $(WEB_DIR) && $(NPM) run build

.PHONY: web-test
web-test: ## run vitest
	cd $(WEB_DIR) && $(NPM) run test:run

.PHONY: e2e
e2e: ## run playwright e2e tests
	cd $(WEB_DIR) && $(NPM) run e2e

.PHONY: embed
embed: web-build ## copy web/dist into internal/web/dist for embed
	rm -rf $(EMBED_DIR)
	mkdir -p $(EMBED_DIR)
	cp -R $(WEB_DIR)/dist/. $(EMBED_DIR)/

.PHONY: build
build: ## build the ember binary
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/ember

.PHONY: run
run: build ## run the ember binary
	EMBER_TEST_MODE=1 $(BIN)

.PHONY: docker
docker: ## build the docker image
	docker build -t ember:$(VERSION) -f Dockerfile .

.PHONY: clean
clean: ## remove build artifacts
	rm -rf ./bin $(COVER_OUT) $(COVER_HTML) $(EMBED_DIR) $(WEB_DIR)/dist $(WEB_DIR)/node_modules

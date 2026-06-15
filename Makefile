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
DOCS_DIR      := docs
DOCS_OUT      := $(DOCS_DIR)/.vitepress/dist

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-16s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ----- Go ----------------------------------------------------------------

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: vet
vet: ## go vet
	$(GO) vet $(PKG)

.PHONY: lint
lint: ## golangci-lint run (requires golangci-lint)
	golangci-lint run

.PHONY: vulncheck
vulncheck: ## scan for known Go CVEs (requires govulncheck)
	govulncheck $(PKG)

.PHONY: test
test: ## go test (race detector, no cache)
	$(GO) test -race -count=1 $(PKG)

.PHONY: cover
cover: ## go test with coverage report
	$(GO) test -race -count=1 -covermode=atomic -coverprofile=$(COVER_OUT) $(PKG)
	$(GO) tool cover -func=$(COVER_OUT) | tail -n 20
	$(GO) tool cover -html=$(COVER_OUT) -o $(COVER_HTML)

.PHONY: verify
verify: vet test ## run vet + tests (what CI runs)

# ----- Web ---------------------------------------------------------------

.PHONY: web-install
web-install: ## install web deps
	cd $(WEB_DIR) && $(NPM) install

.PHONY: web-build
web-build: ## build the svelte SPA to web/dist
	cd $(WEB_DIR) && $(NPM) run build

.PHONY: web-check
web-check: ## svelte-check (TypeScript + a11y warnings)
	cd $(WEB_DIR) && $(NPM) run check

.PHONY: web-test
web-test: ## run vitest
	cd $(WEB_DIR) && $(NPM) run test:run

.PHONY: e2e-install
e2e-install: ## install playwright + chromium
	cd $(WEB_DIR) && $(NPM) run e2e:install

.PHONY: e2e
e2e: ## run playwright e2e tests
	cd $(WEB_DIR) && $(NPM) run e2e

# ----- Binary ------------------------------------------------------------

.PHONY: embed
embed: web-build ## copy web/dist into internal/web/dist for embed
	rm -rf $(EMBED_DIR)
	mkdir -p $(EMBED_DIR)
	cp -R $(WEB_DIR)/dist/. $(EMBED_DIR)/

.PHONY: build
build: ## build the ember binary
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/ember

.PHONY: run
run: build ## build then run with seeded test data
	EMBER_TEST_MODE=1 $(BIN)

# ----- Local feature sandbox --------------------------------------------
# Always builds from the develop branch (via `git archive develop`), so the
# stack reflects develop regardless of your current branch/working tree.
SANDBOX_PORT    ?= 8095
SANDBOX_COMPOSE := docker compose -f deploy/docker-compose.sandbox.yml

.PHONY: sandbox
sandbox: ## build develop into an isolated, fully-seeded compose stack (http://localhost:8095)
	@git rev-parse --verify --quiet develop >/dev/null || { echo "error: no local 'develop' branch to build from"; exit 1; }
	@echo ">>> building ember:sandbox from develop ($$(git rev-parse --short develop))"
	@git archive develop | docker build -q -f Dockerfile \
		--build-arg VERSION="$$(git describe --tags --always develop 2>/dev/null || echo dev)" \
		-t ember:sandbox - >/dev/null
	@echo ">>> (re)creating sandbox stack"
	@$(SANDBOX_COMPOSE) down -v >/dev/null 2>&1 || true
	@SANDBOX_PORT=$(SANDBOX_PORT) $(SANDBOX_COMPOSE) up -d
	@echo ">>> waiting for ember on :$(SANDBOX_PORT) ..."
	@for i in $$(seq 1 60); do \
		curl -fsS "http://localhost:$(SANDBOX_PORT)/healthz" >/dev/null 2>&1 && break; \
		[ $$i = 60 ] && { echo "ember did not become healthy"; $(SANDBOX_COMPOSE) logs ember | tail -20; exit 1; }; \
		sleep 2; \
	done
	@echo ">>> seeding all features"
	@$(SANDBOX_COMPOSE) exec -T ember /ember seed
	@echo ""
	@echo "  Sandbox ready:  http://localhost:$(SANDBOX_PORT)"
	@echo "    admin user:   admin / admintest"
	@echo "    second user:  reader / readerpass"
	@echo "  Live AI summaries: the model pull runs in the background (first run: a few min)."
	@echo "  Tear down + wipe:  make sandbox-down"

.PHONY: sandbox-down
sandbox-down: ## stop the sandbox stack and wipe its data + volumes
	$(SANDBOX_COMPOSE) down -v

.PHONY: docker
docker: ## build the docker image
	docker build -t ember:$(VERSION) -f Dockerfile .

.PHONY: changelog-release
changelog-release: ## update CHANGELOG link refs for a release: make changelog-release VERSION=vX.Y.Z
	@scripts/changelog-release.sh $(VERSION)

.PHONY: release-local
release-local: embed ## cross-compile release binaries to dist/ (no upload, mirrors CI)
	@rm -rf dist
	@mkdir -p dist
	@for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo ">>> $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/ember ./cmd/ember || exit 1; \
		tar -czf dist/ember-$(VERSION)-$$os-$$arch.tar.gz -C dist ember; \
		rm dist/ember; \
	done
	@cd dist && shasum -a 256 ember-*.tar.gz | sort -k2 > SHA256SUMS
	@ls -la dist/

# ----- Docs site (VitePress) --------------------------------------------

.PHONY: docs-install
docs-install: ## install docs site deps (one-time)
	cd $(DOCS_DIR) && $(NPM) install

.PHONY: docs-screenshots
docs-screenshots: ## capture UI screenshots into docs/public/screenshots (requires running docker stack)
	cd $(WEB_DIR) && node scripts/screenshots.mjs

.PHONY: docs-social-preview
docs-social-preview: ## regenerate docs/public/social-preview.png (1280x640 GitHub OG card)
	cd $(WEB_DIR) && node scripts/social-preview.mjs

.PHONY: docs-dev
docs-dev: ## VitePress dev server (hot reload)
	cd $(DOCS_DIR) && $(NPM) run docs:dev

.PHONY: docs-build
docs-build: ## build the docs site to $(DOCS_OUT)
	cd $(DOCS_DIR) && $(NPM) run docs:build

.PHONY: docs-preview
docs-preview: docs-build ## serve the built site locally
	cd $(DOCS_DIR) && $(NPM) run docs:preview

# ----- Misc --------------------------------------------------------------

.PHONY: clean
clean: ## remove build artifacts (leaves installed deps so you can rebuild immediately)
	rm -rf ./bin $(COVER_OUT) $(COVER_HTML) $(EMBED_DIR) $(WEB_DIR)/dist $(DOCS_OUT)

.PHONY: distclean
distclean: clean ## also remove installed dependencies (full reset; re-run make web-install after)
	rm -rf $(WEB_DIR)/node_modules $(DOCS_DIR)/node_modules

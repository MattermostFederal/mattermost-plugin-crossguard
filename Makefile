GO ?= $(shell command -v go 2> /dev/null)
NPM ?= $(shell command -v npm 2> /dev/null)
CURL ?= $(shell command -v curl 2> /dev/null)
MM_DEBUG ?=
GOPATH ?= $(shell go env GOPATH)
GO_TEST_FLAGS ?= -race
GO_BUILD_FLAGS ?=
DEFAULT_GOOS := $(shell go env GOOS)
DEFAULT_GOARCH := $(shell go env GOARCH)

export GO111MODULE=on

# We need to export GOBIN to allow it to be set
# for processes spawned from the Makefile
export GOBIN ?= $(PWD)/bin

# You can include assets this directory into the bundle. This can be e.g. used to include profile pictures.
ASSETS_DIR ?= assets

# Verify environment, and define PLUGIN_ID, PLUGIN_VERSION, HAS_SERVER and HAS_WEBAPP as needed.
include build/setup.mk

BUNDLE_NAME ?= $(PLUGIN_ID)-$(PLUGIN_VERSION).tar.gz

# Include custom makefile, if present
ifneq ($(wildcard build/custom.mk),)
	include build/custom.mk
endif

ifneq ($(MM_DEBUG),)
	GO_BUILD_GCFLAGS = -gcflags "all=-N -l"
else
	GO_BUILD_GCFLAGS =
endif

# ====================================================================================
# Default Target
# ====================================================================================

.PHONY: default
default: all

# ====================================================================================
# Build Targets
# ====================================================================================

## Checks the code style, tests, builds and bundles the plugin.
.PHONY: all
all: check-style test dist

## Pre-release checks: git status and changelog validation.
.PHONY: release-check
release-check:
	@echo "Running pre-release checks..."
	@if [ -n "$$(git status --porcelain -- . ':!webapp/package-lock.json')" ]; then \
		echo "ERROR: Working directory has uncommitted changes."; \
		echo "Please commit or stash changes before building a release."; \
		git status --short -- . ':!webapp/package-lock.json'; \
		exit 1; \
	fi
	@if [ ! -f CHANGELOG.md ]; then \
		echo "ERROR: CHANGELOG.md not found."; \
		exit 1; \
	fi
	@if ! grep -q "## \[Unreleased\]" CHANGELOG.md && ! grep -q "## \[$(PLUGIN_VERSION)\]" CHANGELOG.md; then \
		echo "WARNING: CHANGELOG.md may not be updated for version $(PLUGIN_VERSION)."; \
	fi
	@echo "Pre-release checks passed."

## Generate SHA256 checksum for the release bundle.
.PHONY: release-checksum
release-checksum:
	@echo "Generating SHA256 checksum..."
	@cd dist && shasum -a 256 $(BUNDLE_NAME) > $(BUNDLE_NAME).sha256
	@echo "Checksum: $$(cat dist/$(BUNDLE_NAME).sha256)"

## Include SBOMs and CodeQL results in the release bundle and repackage.
.PHONY: release-bundle
release-bundle:
	@echo "Including SBOMs and security reports in release bundle..."
	@if [ -d dist/sbom ]; then \
		cp -r dist/sbom dist/$(PLUGIN_ID)/; \
		echo "SBOMs included in bundle"; \
	else \
		echo "WARNING: No SBOMs found to include"; \
	fi
	@mkdir -p dist/$(PLUGIN_ID)/security
	@if [ -f dist/codeql-go.sarif ]; then \
		cp dist/codeql-go.sarif dist/$(PLUGIN_ID)/security/; \
		echo "Go CodeQL results included"; \
	fi
	@if [ -f dist/codeql-js.sarif ]; then \
		cp dist/codeql-js.sarif dist/$(PLUGIN_ID)/security/; \
		echo "JavaScript CodeQL results included"; \
	fi
	@rm -f dist/$(BUNDLE_NAME)
	@if [ "$$(uname)" = "Darwin" ]; then \
		cd dist && tar --disable-copyfile -cvzf $(BUNDLE_NAME) $(PLUGIN_ID); \
	else \
		cd dist && tar -cvzf $(BUNDLE_NAME) $(PLUGIN_ID); \
	fi

## Sign the plugin bundle with GPG (requires PLUGIN_SIGNING_KEY env var).
.PHONY: release-sign
release-sign:
	@if [ -n "$(PLUGIN_SIGNING_KEY)" ]; then \
		echo "Signing plugin bundle with GPG key $(PLUGIN_SIGNING_KEY)..."; \
		gpg -u $(PLUGIN_SIGNING_KEY) --verbose --personal-digest-preferences SHA256 --detach-sign dist/$(BUNDLE_NAME); \
		echo "Signature: dist/$(BUNDLE_NAME).sig"; \
	else \
		echo "PLUGIN_SIGNING_KEY not set, skipping signing."; \
		echo "To sign, set PLUGIN_SIGNING_KEY to your GPG key ID."; \
	fi

## Create a git tag for the release version.
.PHONY: release-tag
release-tag:
	@echo "Creating git tag v$(PLUGIN_VERSION)..."
	@if git rev-parse "v$(PLUGIN_VERSION)" >/dev/null 2>&1; then \
		echo "ERROR: Tag v$(PLUGIN_VERSION) already exists."; \
		exit 1; \
	fi
	git tag -a "v$(PLUGIN_VERSION)" -m "Release v$(PLUGIN_VERSION)"
	@echo "Tag v$(PLUGIN_VERSION) created. Push with: git push origin v$(PLUGIN_VERSION)"

## Full release build: clean, checks, style, tests, build, SBOM audit, CodeQL, bundle with SBOMs, sign, and checksum.
.PHONY: release
release: release-check clean all sbom-audit codeql-analyze security-gate release-bundle release-sign release-checksum
	@echo ""
	@echo "=========================================="
	@echo "Release build complete!"
	@echo "Bundle:   dist/$(BUNDLE_NAME)"
	@echo "Checksum: dist/$(BUNDLE_NAME).sha256"
	@if [ -f dist/$(BUNDLE_NAME).sig ]; then echo "Signature: dist/$(BUNDLE_NAME).sig"; fi
	@echo "SBOMs included in bundle"
	@echo ""
	@echo "To tag this release: make release-tag"
	@echo "=========================================="

## Ensures the plugin manifest is valid
.PHONY: manifest-check
manifest-check:
	./build/bin/manifest check

## Propagates plugin manifest information into the server/ and webapp/ folders.
.PHONY: apply
apply:
	./build/bin/manifest apply

## Builds the server, if it exists, for all supported architectures, unless MM_SERVICESETTINGS_ENABLEDEVELOPER is set.
.PHONY: server
server:
ifneq ($(HAS_SERVER),)
ifneq ($(MM_DEBUG),)
	$(info DEBUG mode is on; to disable, unset MM_DEBUG)
endif
	mkdir -p server/dist;
ifneq ($(MM_SERVICESETTINGS_ENABLEDEVELOPER),)
	@echo Building plugin only for $(DEFAULT_GOOS)-$(DEFAULT_GOARCH) because MM_SERVICESETTINGS_ENABLEDEVELOPER is enabled
	cd server && env CGO_ENABLED=0 $(GO) build $(GO_BUILD_FLAGS) $(GO_BUILD_GCFLAGS) -trimpath -o dist/plugin-$(DEFAULT_GOOS)-$(DEFAULT_GOARCH);
else
	cd server && env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GO_BUILD_FLAGS) $(GO_BUILD_GCFLAGS) -trimpath -o dist/plugin-linux-amd64;
	cd server && env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(GO_BUILD_FLAGS) $(GO_BUILD_GCFLAGS) -trimpath -o dist/plugin-linux-arm64;
endif
endif

## Ensures NPM dependencies are installed without having to run this all the time.
webapp/node_modules: $(wildcard webapp/package.json)
ifneq ($(HAS_WEBAPP),)
	cd webapp && $(NPM) install
	touch $@
endif

## Builds the webapp, if it exists.
.PHONY: webapp
webapp: webapp/node_modules
ifneq ($(HAS_WEBAPP),)
ifeq ($(MM_DEBUG),)
	cd webapp && $(NPM) run build;
else
	cd webapp && $(NPM) run debug;
endif
endif

## Generates a tar bundle of the plugin for install.
.PHONY: bundle
bundle:
	rm -rf dist/
	mkdir -p dist/$(PLUGIN_ID)
	./build/bin/manifest dist
ifneq ($(wildcard $(ASSETS_DIR)/.),)
	cp -r $(ASSETS_DIR) dist/$(PLUGIN_ID)/
endif
ifneq ($(HAS_PUBLIC),)
	cp -r public dist/$(PLUGIN_ID)/
endif
ifneq ($(HAS_SERVER),)
	mkdir -p dist/$(PLUGIN_ID)/server
	cp -r server/dist dist/$(PLUGIN_ID)/server/
endif
ifneq ($(HAS_WEBAPP),)
	mkdir -p dist/$(PLUGIN_ID)/webapp
	cp -r webapp/dist dist/$(PLUGIN_ID)/webapp/
endif
ifeq ($(shell uname),Darwin)
	cd dist && tar --disable-copyfile -cvzf $(BUNDLE_NAME) $(PLUGIN_ID)
else
	cd dist && tar -cvzf $(BUNDLE_NAME) $(PLUGIN_ID)
endif

	@echo plugin built at: dist/$(BUNDLE_NAME)

## Builds and bundles the plugin.
.PHONY: dist
dist: apply server webapp bundle

# ====================================================================================
# Quality Targets
# ====================================================================================

## Install go tools
install-go-tools:
	@echo Installing go tools
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0
	$(GO) install gotest.tools/gotestsum@v1.13.0

## Runs eslint and golangci-lint
.PHONY: check-style
check-style: manifest-check apply webapp/node_modules install-go-tools
	@echo Checking for style guide compliance

ifneq ($(HAS_WEBAPP),)
	cd webapp && npm run lint
	cd webapp && npm run check-types
endif

# It's highly recommended to run go-vet first
# to find potential compile errors that could introduce
# weird reports at golangci-lint step
ifneq ($(HAS_SERVER),)
	@echo Running golangci-lint
	$(GO) vet ./server/...
	$(GOBIN)/golangci-lint run ./server/...
endif

## Runs any lints and unit tests defined for the server and webapp, if they exist.
.PHONY: test
test: apply webapp/node_modules install-go-tools
ifneq ($(HAS_SERVER),)
	$(GOBIN)/gotestsum -- -v ./...
endif
ifneq ($(HAS_WEBAPP),)
	cd webapp && $(NPM) run test;
endif

## Runs any lints and unit tests defined for the server and webapp, if they exist, optimized for a CI environment.
.PHONY: test-ci
test-ci: apply webapp/node_modules install-go-tools
ifneq ($(HAS_SERVER),)
	$(GOBIN)/gotestsum --format standard-verbose --junitfile report.xml -- ./...
endif
ifneq ($(HAS_WEBAPP),)
	cd webapp && $(NPM) run test;
endif

## Creates a coverage report for the server code.
.PHONY: coverage
coverage: apply webapp/node_modules
ifneq ($(HAS_SERVER),)
	$(GO) test $(GO_TEST_FLAGS) -coverprofile=server/coverage.txt ./server/...
	$(GO) tool cover -html=server/coverage.txt
endif

## Clean removes all build artifacts (but preserves build tools).
.PHONY: clean
clean:
	rm -fr dist/
ifneq ($(HAS_SERVER),)
	rm -fr server/coverage.txt
	rm -fr server/dist
endif
ifneq ($(HAS_WEBAPP),)
	rm -fr webapp/junit.xml
	rm -fr webapp/dist
	rm -fr webapp/node_modules
endif

## Nuke everything: Docker containers, data, and all build artifacts
.PHONY: nuke
nuke: docker-kill-orphans
	@echo "Nuking everything..."
	@$(DOCKER_COMPOSE) down -v 2>/dev/null || true
	@rm -rf docker/postgres-a-data docker/postgres-b-data docker/mattermost-a docker/mattermost-b
	@rm -fr dist/
	@rm -fr server/coverage.txt server/dist
	@rm -fr webapp/junit.xml webapp/dist webapp/node_modules
	@rm -fr build/bin/
	@echo "Everything removed. Run 'make docker-setup' to start fresh."

# ====================================================================================
# Docker Development Environment (Dual-Server)
# ====================================================================================
DOCKER_COMPOSE := docker compose -f docker-compose.dev.yml
MM_PORT_A ?= 8075
MM_PORT_B ?= 8076

## Start both Mattermost servers and PostgreSQL containers
.PHONY: docker-start
docker-start:
	@echo "Starting dual Mattermost servers..."
	@mkdir -p docker/mattermost-a/{config,data,logs,plugins,client-plugins}
	@mkdir -p docker/mattermost-b/{config,data,logs,plugins,client-plugins}
	@mkdir -p docker/postgres-a-data docker/postgres-b-data
	@$(DOCKER_COMPOSE) up -d

## Stop containers (preserves data)
.PHONY: docker-stop
docker-stop:
	@$(DOCKER_COMPOSE) stop

## Stop and remove containers
.PHONY: docker-down
docker-down:
	@$(DOCKER_COMPOSE) down

## Remove containers and all data
.PHONY: docker-clean
docker-clean:
	@$(DOCKER_COMPOSE) down -v
	@rm -rf docker/postgres-a-data docker/postgres-b-data docker/mattermost-a docker/mattermost-b
	@echo "Containers and data removed"

## Kill orphaned Docker containers on the MM ports (useful after deleting a worktree)
.PHONY: docker-kill-orphans
docker-kill-orphans:
	@for port in $(MM_PORT_A) $(MM_PORT_B); do \
		project=$$(docker ps --filter "publish=$$port" \
			--format '{{.Label "com.docker.compose.project"}}' | head -1); \
		if [ -z "$$project" ]; then \
			echo "No containers found on port $$port"; \
		else \
			echo "Stopping compose project: $$project"; \
			docker compose -p $$project down -v; \
			echo "Project $$project removed"; \
		fi; \
	done

## View Server A (mattermost-a) container logs
.PHONY: docker-logs
docker-logs: docker-check
	@$(DOCKER_COMPOSE) logs -f mattermost-a

## View Server B (mattermost-b) container logs
.PHONY: docker-logs-b
docker-logs-b: docker-check
	@$(DOCKER_COMPOSE) logs -f mattermost-b

## First-time setup: start containers and create users on both servers
.PHONY: docker-setup
docker-setup: docker-start
	@if ! grep -q 'cga.test' /etc/hosts || ! grep -q 'cgb.test' /etc/hosts; then \
		echo ""; \
		echo "WARNING: /etc/hosts is missing cga.test and/or cgb.test entries."; \
		echo "Run 'make hosts-setup' to add them, or the admin console will not work correctly."; \
		echo ""; \
	fi
	@echo "Waiting for Server A (mattermost-a) to be ready..."
	@until curl -sf http://localhost:$(MM_PORT_A)/api/v4/system/ping >/dev/null 2>&1; do \
		sleep 2; \
		echo "Waiting for Server A..."; \
	done
	@echo "Waiting for Server B (mattermost-b) to be ready..."
	@until curl -sf http://localhost:$(MM_PORT_B)/api/v4/system/ping >/dev/null 2>&1; do \
		sleep 2; \
		echo "Waiting for Server B..."; \
	done
	@echo ""
	@echo "--- Setting up Server A ---"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email admin@example.com \
		--username admin \
		--password 'password' \
		--system-admin 2>/dev/null || echo "Admin user already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email usera@example.com \
		--username usera \
		--password 'password' 2>/dev/null || echo "User usera already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team create \
		--name test \
		--display-name "Test A" 2>/dev/null || echo "Team 'Test A' already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test admin 2>/dev/null || echo "Admin already in Test A team"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test usera 2>/dev/null || echo "usera already in Test A team"
	@echo ""
	@echo "--- Setting up Server B ---"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local user create \
		--email admin@example.com \
		--username admin \
		--password 'password' \
		--system-admin 2>/dev/null || echo "Admin user already exists on Server B"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local user create \
		--email userb@example.com \
		--username userb \
		--password 'password' 2>/dev/null || echo "User userb already exists on Server B"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local team create \
		--name test \
		--display-name "Test B" 2>/dev/null || echo "Team 'Test B' already exists on Server B"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local team users add test admin 2>/dev/null || echo "Admin already in Test B team"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local team users add test userb 2>/dev/null || echo "userb already in Test B team"
	@echo ""
	@echo "=========================================="
	@echo "Server A (CGA): http://cga.test:$(MM_PORT_A)"
	@echo "  Admin login: admin / password"
	@echo "  User login:  usera / password"
	@echo "  Team:        Test A"
	@echo ""
	@echo "Server B (CGB): http://cgb.test:$(MM_PORT_B)"
	@echo "  Admin login: admin / password"
	@echo "  User login:  userb / password"
	@echo "  Team:        Test B"
	@echo ""
	@echo "NATS: nats://localhost:$${NATS_PORT:-4222}"
	@echo "NATS Monitor: http://localhost:$${NATS_MONITOR_PORT:-8222}"
	@echo "NATS (from plugins): nats://nats:4222"
	@echo "=========================================="
	@echo ""
	@echo "Next: run 'make deploy' to build, deploy, and configure NATS connections."

## Check if both Mattermost containers are running
.PHONY: docker-check
docker-check:
	@if ! $(DOCKER_COMPOSE) ps --status running 2>/dev/null | grep -q mattermost-a; then \
		echo "Error: mattermost-a container is not running."; \
		echo "Run 'make docker-setup' first to start the environment."; \
		exit 1; \
	fi
	@if ! $(DOCKER_COMPOSE) ps --status running 2>/dev/null | grep -q mattermost-b; then \
		echo "Error: mattermost-b container is not running."; \
		echo "Run 'make docker-setup' first to start the environment."; \
		exit 1; \
	fi

## Build and deploy plugin to both Docker Mattermost servers
.PHONY: docker-deploy
docker-deploy: docker-check dist
	@echo "Deploying plugin to Server A (mattermost-a)..."
	@$(DOCKER_COMPOSE) cp dist/$(BUNDLE_NAME) mattermost-a:/tmp/$(BUNDLE_NAME)
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin add /tmp/$(BUNDLE_NAME) --force
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID)
	@echo "Plugin $(PLUGIN_ID) deployed and enabled on Server A"
	@echo "Deploying plugin to Server B (mattermost-b)..."
	@$(DOCKER_COMPOSE) cp dist/$(BUNDLE_NAME) mattermost-b:/tmp/$(BUNDLE_NAME)
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin add /tmp/$(BUNDLE_NAME) --force
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID)
	@echo "Plugin $(PLUGIN_ID) deployed and enabled on Server B"
	@echo ""
	@echo "Configuring NATS connections..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	curl -sf -X PUT http://localhost:$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"[{\"name\":\"relay-test\",\"address\":\"nats://nats:4222\",\"subject\":\"crossguard.relay\",\"auth_type\":\"none\"}]","inboundconnections":"[]"}}}}' >/dev/null && \
	echo "Server A configured with outbound NATS connection"
	@TOKEN_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	curl -sf -X PUT http://localhost:$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"[{\"name\":\"relay-test\",\"address\":\"nats://nats:4222\",\"subject\":\"crossguard.relay\",\"auth_type\":\"none\"}]","outboundconnections":"[]"}}}}' >/dev/null && \
	echo "Server B configured with inbound NATS connection"

## End-to-end smoke test: init teams/channels, post message on A, verify relay to B
.PHONY: docker-smoke-test
docker-smoke-test: docker-check
	@echo ""
	@echo "Running end-to-end smoke test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	CHAN_A=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/off-topic \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	CHAN_B=$$(curl -sf http://localhost:$(MM_PORT_B)/api/v4/teams/name/test/channels/name/off-topic \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Initializing teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$CHAN_A"'","command":"/crossguard init-team outbound:relay-test"}' >/dev/null && \
	echo "  Server A: init-team outbound:relay-test" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$CHAN_B"'","command":"/crossguard init-team inbound:relay-test"}' >/dev/null && \
	echo "  Server B: init-team inbound:relay-test" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$CHAN_A"'","command":"/crossguard init-channel outbound:relay-test"}' >/dev/null && \
	echo "  Server A: init-channel outbound:relay-test on off-topic" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$CHAN_B"'","command":"/crossguard init-channel inbound:relay-test"}' >/dev/null && \
	echo "  Server B: init-channel inbound:relay-test on off-topic" && \
	echo "Posting smoke-test message from Server A..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SMOKE_ID=$$(date +%s)-$$$$ && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$CHAN_A"'","message":"smoke-test:'"$$SMOKE_ID"'"}' >/dev/null && \
	echo "  Posted smoke-test:$$SMOKE_ID to Server A off-topic" && \
	echo "Waiting for relay..." && \
	sleep 3 && \
	FOUND=$$(curl -sf "http://localhost:$(MM_PORT_B)/api/v4/channels/$$CHAN_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$SMOKE_ID';found=any('smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Smoke test result: $$FOUND" || \
	{ echo "Smoke test FAILED: message smoke-test:$$SMOKE_ID not found on Server B"; exit 1; }

## Disable and re-enable plugin on both servers
.PHONY: docker-reset
docker-reset: docker-check
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID)
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID)
	@echo "Plugin $(PLUGIN_ID) reset on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID)
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID)
	@echo "Plugin $(PLUGIN_ID) reset on Server B"

## Disable plugin on both servers
.PHONY: docker-disable
docker-disable: docker-check
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID)
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID)

## Enable plugin on both servers
.PHONY: docker-enable
docker-enable: docker-check
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID)
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID)

## List installed plugins on both servers
.PHONY: docker-plugin-list
docker-plugin-list: docker-check
	@echo "--- Server A ---"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin list
	@echo "--- Server B ---"
	@$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin list

## Convenience alias: deploy plugin to Docker and run smoke test
.PHONY: deploy
deploy: docker-deploy docker-smoke-test

## Print /etc/hosts entries needed for dual-server setup
.PHONY: hosts-setup
hosts-setup:
	@if grep -q 'cga.test' /etc/hosts && grep -q 'cgb.test' /etc/hosts; then \
		echo "/etc/hosts already has cga.test and cgb.test entries."; \
	else \
		echo "Adding cga.test and cgb.test to /etc/hosts (requires sudo)..."; \
		echo '127.0.0.1  cga.test cgb.test' | sudo tee -a /etc/hosts; \
		echo "Done. /etc/hosts updated."; \
	fi

# ====================================================================================
# SBOM & Vulnerability Scanning
# ====================================================================================

## Install SBOM generation tools
.PHONY: install-sbom-tools
install-sbom-tools:
	@echo "Installing SBOM generation tools..."
	$(GO) install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest

## Install Grype vulnerability scanner
.PHONY: install-grype
install-grype:
	@if ! command -v $(GOBIN)/grype >/dev/null 2>&1; then \
		echo "Installing Grype..."; \
		curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b $(GOBIN); \
	else \
		echo "Grype already installed"; \
	fi

## Generate Software Bill of Materials (SBOM) in CycloneDX JSON format
.PHONY: sbom
sbom: install-sbom-tools
	@mkdir -p dist/sbom
ifneq ($(HAS_SERVER),)
	@echo "Generating Go SBOM..."
	$(GOBIN)/cyclonedx-gomod mod -json -output dist/sbom/server-sbom.json
endif
ifneq ($(HAS_WEBAPP),)
	@echo "Generating Node.js SBOM..."
	cd webapp && npx @cyclonedx/cyclonedx-npm --ignore-npm-errors --output-file ../dist/sbom/webapp-sbom.json
endif
	@echo "SBOMs generated in dist/sbom/"
	@ls -la dist/sbom/

## Scan SBOMs for vulnerabilities using Grype (fails on high or critical)
.PHONY: sbom-scan
sbom-scan: install-grype
	@if [ ! -d dist/sbom ]; then \
		echo "No SBOMs found. Run 'make sbom' first."; \
		exit 1; \
	fi
ifneq ($(HAS_SERVER),)
	@echo "Scanning Go dependencies for vulnerabilities..."
	$(GOBIN)/grype sbom:dist/sbom/server-sbom.json --output table --fail-on high
endif
ifneq ($(HAS_WEBAPP),)
	@echo "Scanning Node.js dependencies for vulnerabilities..."
	$(GOBIN)/grype sbom:dist/sbom/webapp-sbom.json --output table --fail-on high
endif

## Generate SBOMs and scan for vulnerabilities
.PHONY: sbom-audit
sbom-audit: sbom sbom-scan

# ====================================================================================
# CodeQL Security Analysis
# ====================================================================================

CODEQL_VERSION ?= 2.20.1
CODEQL_DIR := $(PWD)/build/codeql
CODEQL := $(CODEQL_DIR)/codeql/codeql
CODEQL_DB_DIR := $(PWD)/build/codeql-db

## Install CodeQL CLI
.PHONY: install-codeql
install-codeql:
	@if [ ! -f "$(CODEQL)" ]; then \
		echo "Installing CodeQL CLI v$(CODEQL_VERSION)..."; \
		mkdir -p $(CODEQL_DIR); \
		if [ "$$(uname)" = "Darwin" ]; then \
			if [ "$$(uname -m)" = "arm64" ]; then \
				CODEQL_PLATFORM="osx64"; \
			else \
				CODEQL_PLATFORM="osx64"; \
			fi; \
		else \
			CODEQL_PLATFORM="linux64"; \
		fi; \
		curl -sSL "https://github.com/github/codeql-action/releases/download/codeql-bundle-v$(CODEQL_VERSION)/codeql-bundle-$$CODEQL_PLATFORM.tar.gz" | tar -xz -C $(CODEQL_DIR); \
		echo "CodeQL CLI installed"; \
	else \
		echo "CodeQL CLI already installed"; \
	fi

## Run CodeQL analysis on Go code
.PHONY: codeql-go
codeql-go: install-codeql
ifneq ($(HAS_SERVER),)
	@echo "Running CodeQL analysis on Go code..."
	@rm -rf $(CODEQL_DB_DIR)/go
	@mkdir -p $(CODEQL_DB_DIR)/go
	$(CODEQL) database create $(CODEQL_DB_DIR)/go --language=go --source-root=server --overwrite
	$(CODEQL) database analyze $(CODEQL_DB_DIR)/go --format=sarif-latest --output=dist/codeql-go.sarif -- codeql/go-queries
	@echo "Go CodeQL results: dist/codeql-go.sarif"
endif

## Run CodeQL analysis on JavaScript/TypeScript code
.PHONY: codeql-js
codeql-js: install-codeql webapp/node_modules
ifneq ($(HAS_WEBAPP),)
	@echo "Running CodeQL analysis on JavaScript/TypeScript code..."
	@rm -rf $(CODEQL_DB_DIR)/js
	@mkdir -p $(CODEQL_DB_DIR)/js
	$(CODEQL) database create $(CODEQL_DB_DIR)/js --language=javascript --source-root=webapp --overwrite
	$(CODEQL) database analyze $(CODEQL_DB_DIR)/js --format=sarif-latest --output=dist/codeql-js.sarif -- codeql/javascript-queries
	@echo "JavaScript/TypeScript CodeQL results: dist/codeql-js.sarif"
endif

## Run CodeQL analysis on all code
.PHONY: codeql-analyze
codeql-analyze: codeql-go codeql-js
	@echo "CodeQL analysis complete. Results in dist/codeql-*.sarif"

## Check CodeQL SARIF reports for critical/high severity issues (level=error in SARIF)
.PHONY: security-gate
security-gate:
	@echo "Checking security scan results for critical/high issues..."
	@failed=0; \
	for sarif in dist/codeql-*.sarif; do \
		[ -f "$$sarif" ] || continue; \
		count=$$(python3 -c "import json,sys;data=json.load(open(sys.argv[1]));print(sum(1 for run in data.get('runs',[]) for result in run.get('results',[]) if result.get('level')=='error'))" "$$sarif"); \
		if [ "$$count" -gt 0 ]; then \
			echo "ERROR: $$sarif contains $$count critical/high severity issue(s)."; \
			failed=1; \
		else \
			echo "OK: $$sarif has no critical/high severity issues."; \
		fi; \
	done; \
	if [ "$$failed" -eq 1 ]; then \
		echo ""; \
		echo "Security gate FAILED: Critical or high severity issues found."; \
		echo "Review the SARIF files in dist/ for details."; \
		exit 1; \
	fi
	@echo "Security gate passed."

# ====================================================================================
# Help
# ====================================================================================

help:
	@cat Makefile build/*.mk | grep -v '\.PHONY' |  grep -v '\help:' | grep -B1 -E '^[a-zA-Z0-9_.-]+:.*' | sed -e "s/:.*//" | sed -e "s/^## //" |  grep -v '\-\-' | sed '1!G;h;$$!d' | awk 'NR%2{printf "\033[36m%-30s\033[0m",$$0;next;}1' | sort

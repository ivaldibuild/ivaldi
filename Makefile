CLI_BINARY_NAME := ivaldictl
SERVER_BINARY_NAME := ivaldi
K8S_AGENT_BINARY_NAME := ivaldi-k8s-agent

# Stamped into both binaries' --version.
VERSION ?= $(shell scripts/version.sh)
LDFLAGS := -ldflags "-X main.VERSION=$(VERSION)"

# Control-plane capabilities with their own migrations, in the order they are applied.
# There are no foreign keys between capabilities, so the order is for readability only.
CAPABILITIES := outbox audit iam workspace versioncontrol delivery provenance team policy orchestration

MIGRATIONS_ROOT := apps/control-plane/internal

# golang-migrate CLI (https://github.com/golang-migrate/migrate), pinned in mise.toml.
MIGRATE ?= migrate

# Each capability keeps its migration versions in its own table in the public schema,
# because every capability numbers its migrations from 000001. See
# docs/design/delivery-data-model.md, "Appendix: planned schema".
migrations_dir = $(MIGRATIONS_ROOT)/$(1)/migrations
migrations_table = $(if $(filter versioncontrol,$(1)),version_control,$(1))_schema_migrations

# Appends x-migrations-table to DATABASE_URL, whether or not it already has a query string.
database_url = $$(case "$(DATABASE_URL)" in *\?*) printf '%s&' "$(DATABASE_URL)";; *) printf '%s?' "$(DATABASE_URL)";; esac)x-migrations-table=$(call migrations_table,$(1))

# Local development: PostgreSQL and Valkey from docker-compose.yml, and the
# control plane configured by config.local.yml, which starts as a copy of
# config.local.example.yml. Generated secrets live in .local/. Git ignores both.
LOCAL_CONFIG := config.local.yml
LOCAL_DIR := .local
PG_PASSWORD ?= brokandsindri
LOCAL_DATABASE_URL ?= postgres://ivaldi_db_user:$(PG_PASSWORD)@127.0.0.1:5433/ivaldi?sslmode=disable

.PHONY: help prepare build build-cli build-server openapi openapi_check k8s_agent_e2e migration_generate migrate_up migrate_down migrate_version check_capability check_database_url \
	local local_up local_stack local_secrets local_database local_migrate local_serve local_gateway local_down local_reset

help:
	@echo "Build (into bin/, version $(VERSION))"
	@echo "  make build                                         Build $(SERVER_BINARY_NAME), $(CLI_BINARY_NAME), and $(K8S_AGENT_BINARY_NAME)"
	@echo "  make build-server                                  Build $(SERVER_BINARY_NAME): control plane, gateway, worker, indexer"
	@echo "  make build-cli                                     Build $(CLI_BINARY_NAME)"
	@echo "  make build-k8s-agent                               Build $(K8S_AGENT_BINARY_NAME)"
	@echo ""
	@echo "API clients"
	@echo "  make openapi                                       Regenerate api/openapi.json, the Go clients, and the web app's types"
	@echo "  make openapi_check                                 Fail if any of them is out of date"
	@echo ""
	@echo "End-to-end"
	@echo "  make k8s_agent_e2e                                 Run the agent against a kind cluster and a real control"
	@echo "                                                     plane (needs docker, kind, helm); IVALDI_E2E_KEEP=1 keeps them"
	@echo ""
	@echo "Migrations (capabilities: $(CAPABILITIES))"
	@echo "  make <capability>_migration_generate name=<name>   Create a numbered up/down pair"
	@echo "  make migrate_up DATABASE_URL=<url> [capability=<c>] Apply all, or one capability's, migrations"
	@echo "  make migrate_down DATABASE_URL=<url> capability=<c> [steps=<n>]"
	@echo "                                                     Roll back one capability (default: 1 step)"
	@echo "  make migrate_version DATABASE_URL=<url>            Show each capability's current version"
	@echo ""
	@echo "Local development ($(LOCAL_CONFIG))"
	@echo "  make local                                         Start PostgreSQL and Valkey, generate secrets, migrate"
	@echo "  make local_serve                                   Run the control plane on http://localhost:8660"
	@echo "  make local_gateway                                 Run the gateway on http://localhost:8663; uncomment the"
	@echo "                                                     gateway section in $(LOCAL_CONFIG) first"
	@echo "  make local_stack                                   Run everything in containers (docker-compose.yml): the"
	@echo "                                                     web app on http://localhost:8670, the control plane on 8660,"
	@echo "                                                     the gateway on 8663"
	@echo "  make local_down                                    Stop the containers (data stays in volumes/)"
	@echo "  make local_reset                                   Drop the local database and migrate a fresh one, so"
	@echo "                                                     the next start bootstraps from $(LOCAL_CONFIG) again"

prepare:
	@echo "Preparing build environment..."
	@mkdir -p bin

build: build-server build-cli build-k8s-agent

build-cli: prepare
	@echo "Building CLI binary..."
	@go build $(LDFLAGS) -o bin/$(CLI_BINARY_NAME) ./apps/cli/cmd

build-k8s-agent: prepare
	@echo "Building Kubernetes agent binary..."
	@go build $(LDFLAGS) -o bin/$(K8S_AGENT_BINARY_NAME) ./apps/k8s-agent/cmd

build-server: prepare
	@echo "Building server binary..."
	@go build $(LDFLAGS) -o bin/$(SERVER_BINARY_NAME) ./apps/control-plane/cmd

# make iam_migration_generate name=create_agents_table
%_migration_generate:
	@$(MAKE) --no-print-directory migration_generate capability=$* name="$(name)"

migration_generate: check_capability
	@if [ -z "$(name)" ]; then \
		echo "Error: migration name is required. Usage: make $(capability)_migration_generate name=<migration_name>"; \
		exit 1; \
	fi
	@echo "Generating $(capability) migration: $(name)"
	@$(MIGRATE) create -ext sql -dir $(call migrations_dir,$(capability)) -seq $(name)

migrate_up: check_database_url
	@for c in $(or $(capability),$(CAPABILITIES)); do \
		$(MAKE) --no-print-directory check_capability capability=$$c || exit 1; \
	done
	@$(foreach c,$(or $(capability),$(CAPABILITIES)), \
		echo "== $(c)" && $(MIGRATE) -path $(call migrations_dir,$(c)) -database "$(call database_url,$(c))" up || exit 1;)

migrate_down: check_database_url check_capability
	@echo "== $(capability): rolling back $(or $(steps),1) migration(s)"
	@$(MIGRATE) -path $(call migrations_dir,$(capability)) -database "$(call database_url,$(capability))" down $(or $(steps),1)

migrate_version: check_database_url
	@$(foreach c,$(CAPABILITIES), \
		printf '%-16s ' "$(c)" && ($(MIGRATE) -path $(call migrations_dir,$(c)) -database "$(call database_url,$(c))" version 2>&1 || true);)

check_capability:
	@if [ -z "$(capability)" ]; then \
		echo "Error: capability is required. One of: $(CAPABILITIES)"; \
		exit 1; \
	fi
	@if ! echo " $(CAPABILITIES) " | grep -q " $(capability) "; then \
		echo "Error: unknown capability '$(capability)'. One of: $(CAPABILITIES)"; \
		exit 1; \
	fi
	@if [ ! -d "$(call migrations_dir,$(capability))" ]; then \
		echo "Error: $(call migrations_dir,$(capability)) does not exist"; \
		exit 1; \
	fi

check_database_url:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required, e.g. postgres://user:pass@localhost:5432/ivaldi?sslmode=disable"; \
		exit 1; \
	fi

local: $(LOCAL_CONFIG) local_up local_secrets local_database local_migrate
	@echo ""
	@echo "Ready. Run 'make local_serve', then sign in as bootstrap.admin_email from"
	@echo "$(LOCAL_CONFIG) with the password in $(LOCAL_DIR)/admin-password."

# Copied once and never overwritten: edit the copy, not the example.
$(LOCAL_CONFIG):
	@cp config.local.example.yml $@
	@echo "Created $@ from config.local.example.yml"

local_up:
	@docker compose up -d --wait postgres redis

# PostgreSQL, Valkey, the migrations, the control plane, and the web app, all
# in containers. The control plane reads $(LOCAL_CONFIG) as local_serve does,
# and runs as you so it can read .local/.
local_stack: $(LOCAL_CONFIG) local_secrets
	@IVALDI_UID=$$(id -u) IVALDI_GID=$$(id -g) docker compose up -d --build --wait
	@echo ""
	@echo "Ready. Open http://localhost:8670 and sign in as bootstrap.admin_email from"
	@echo "$(LOCAL_CONFIG) with the password in $(LOCAL_DIR)/admin-password."

# Secrets are generated once and kept, so tokens and the admin password
# survive restarts.
local_secrets: $(LOCAL_DIR)/signing.pem $(LOCAL_DIR)/admin-password $(LOCAL_DIR)/valkey-password \
	$(LOCAL_DIR)/gateway-key.pub $(LOCAL_DIR)/github-webhook-secret

$(LOCAL_DIR):
	@mkdir -p $@ && chmod 700 $@

$(LOCAL_DIR)/signing.pem: | $(LOCAL_DIR)
	@openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out $@ 2>/dev/null && chmod 600 $@
	@echo "Generated the token signing key in $@"

$(LOCAL_DIR)/admin-password: | $(LOCAL_DIR)
	@openssl rand -base64 18 > $@ && chmod 600 $@
	@echo "Generated the bootstrap admin password in $@"

$(LOCAL_DIR)/valkey-password: conf/valkey.conf | $(LOCAL_DIR)
	@sed -n 's/^requirepass //p' conf/valkey.conf > $@ && chmod 600 $@

# The gateway's identity as a platform workload (ADR 0015): it keeps the
# private key, and the control plane trusts the public key.
$(LOCAL_DIR)/gateway-key.pem: | $(LOCAL_DIR)
	@openssl genpkey -algorithm ed25519 -out $@ && chmod 600 $@
	@echo "Generated the gateway's private key in $@"

$(LOCAL_DIR)/gateway-key.pub: $(LOCAL_DIR)/gateway-key.pem
	@openssl pkey -in $< -pubout -out $@

$(LOCAL_DIR)/github-webhook-secret: | $(LOCAL_DIR)
	@openssl rand -hex 32 > $@ && chmod 600 $@
	@echo "Generated a GitHub webhook secret in $@; set the same value in the GitHub App"

# A fresh volume creates the database itself (POSTGRES_DB); an older one may
# not have it.
local_database:
	@docker compose exec -T postgres psql -U ivaldi_db_user -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'ivaldi'" | grep -q 1 \
		|| docker compose exec -T postgres createdb -U ivaldi_db_user ivaldi

local_migrate:
	@$(MAKE) --no-print-directory migrate_up DATABASE_URL='$(LOCAL_DATABASE_URL)'

local_serve: $(LOCAL_CONFIG)
	@go run $(LDFLAGS) ./apps/control-plane/cmd serve --config $(LOCAL_CONFIG)

local_gateway: $(LOCAL_CONFIG) local_secrets
	@go run $(LDFLAGS) ./apps/control-plane/cmd gateway --config $(LOCAL_CONFIG)

local_down:
	@docker compose down

# Bootstrap applies only to an empty database, so changing the bootstrap
# settings needs a fresh one.
local_reset:
	@docker compose exec -T postgres dropdb -U ivaldi_db_user --if-exists --force ivaldi
	@$(MAKE) --no-print-directory local_database local_migrate

# The control plane's API description is committed as api/openapi.json, and
# ivaldictl's and the Kubernetes agent's Go clients and the web app's
# TypeScript types are generated from it. Run this after changing an operation
# or a response.
openapi:
	go run ./apps/control-plane/cmd openapi > api/openapi.json
	cd apps/cli && go generate ./internal/apiclient
	cd apps/k8s-agent && go generate ./internal/apiclient
	cd apps/webui && pnpm run api:types

# The Kubernetes agent's end-to-end test: a kind cluster, a registry, and
# the control plane in containers (apps/k8s-agent/e2e/run.sh).
k8s_agent_e2e:
	apps/k8s-agent/e2e/run.sh

# Regenerates everything and fails if that changed a committed file.
openapi_check: openapi
	git diff --exit-code -- api/openapi.json apps/cli/internal/apiclient apps/k8s-agent/internal/apiclient apps/webui/src/app/api/schema.gen.ts

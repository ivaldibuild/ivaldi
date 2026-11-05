CLI_BINARY_NAME := ivaldictl
SERVER_BINARY_NAME := ivaldi

# Stamped into both binaries' --version.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.VERSION=$(VERSION)"

# Control-plane capabilities with their own migrations, in the order they are applied.
# There are no foreign keys between capabilities, so the order is for readability only.
CAPABILITIES := outbox iam workspace versioncontrol delivery

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
# control plane configured by config.local.yml. Generated secrets live in
# .local/, which git ignores.
LOCAL_CONFIG := config.local.yml
LOCAL_DIR := .local
PG_PASSWORD ?= brokandsindri
LOCAL_DATABASE_URL ?= postgres://ivaldi_db_user:$(PG_PASSWORD)@127.0.0.1:5433/ivaldi?sslmode=disable

.PHONY: help prepare build build-cli build-server migration_generate migrate_up migrate_down migrate_version check_capability check_database_url \
	local local_up local_secrets local_database local_migrate local_serve local_down local_reset

help:
	@echo "Build (into bin/, version $(VERSION))"
	@echo "  make build                                         Build $(SERVER_BINARY_NAME) and $(CLI_BINARY_NAME)"
	@echo "  make build-server                                  Build $(SERVER_BINARY_NAME): control plane, gateway, worker, indexer"
	@echo "  make build-cli                                     Build $(CLI_BINARY_NAME)"
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
	@echo "  make local_down                                    Stop PostgreSQL and Valkey (data stays in volumes/)"
	@echo "  make local_reset                                   Drop the local database and migrate a fresh one, so"
	@echo "                                                     the next start bootstraps from $(LOCAL_CONFIG) again"

prepare:
	@echo "Preparing build environment..."
	@mkdir -p bin

build: build-server build-cli

build-cli: prepare
	@echo "Building CLI binary..."
	@go build $(LDFLAGS) -o bin/$(CLI_BINARY_NAME) ./apps/cli/cmd

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

local: local_up local_secrets local_database local_migrate
	@echo ""
	@echo "Ready. Run 'make local_serve', then sign in as admin@ivaldi.local"
	@echo "with the password in $(LOCAL_DIR)/admin-password."

local_up:
	@docker compose up -d --wait

# Secrets are generated once and kept, so tokens and the admin password
# survive restarts.
local_secrets: $(LOCAL_DIR)/signing.pem $(LOCAL_DIR)/admin-password $(LOCAL_DIR)/valkey-password

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

# A fresh volume creates the database itself (POSTGRES_DB); an older one may
# not have it.
local_database:
	@docker compose exec -T postgres psql -U ivaldi_db_user -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'ivaldi'" | grep -q 1 \
		|| docker compose exec -T postgres createdb -U ivaldi_db_user ivaldi

local_migrate:
	@$(MAKE) --no-print-directory migrate_up DATABASE_URL='$(LOCAL_DATABASE_URL)'

local_serve:
	@go run $(LDFLAGS) ./apps/control-plane/cmd serve --config $(LOCAL_CONFIG)

local_down:
	@docker compose down

# Bootstrap applies only to an empty database, so changing the bootstrap
# settings needs a fresh one.
local_reset:
	@docker compose exec -T postgres dropdb -U ivaldi_db_user --if-exists --force ivaldi
	@$(MAKE) --no-print-directory local_database local_migrate

CLI_BINARY_NAME := ivaldictl
SERVER_BINARY_NAME := ivaldi

# Stamped into both binaries' --version.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.VERSION=$(VERSION)"

# Control-plane capabilities with their own migrations, in the order they are applied.
# There are no foreign keys between capabilities, so the order is for readability only.
CAPABILITIES := iam workspace versioncontrol delivery

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

.PHONY: help prepare build build-cli build-server migration_generate migrate_up migrate_down migrate_version check_capability check_database_url

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

IAM_MIGRATIONS_DIR=apps/control-plane/internal/iam/migrations

iam_migration_generate:
	@if [ -z "$(name) "]; then \
		echo "Error: migration name is required. Usage: make iam_migration_generate name=<migration_name>"; \
		exit 1; \
	fi
	@echo "Generating IAM migration: $(name)"
	@mkdir -p $(IAM_MIGRATIONS_DIR)
	@migrate create -ext sql -dir $(IAM_MIGRATIONS_DIR) -seq $(name)
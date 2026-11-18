// Package migrations holds the notifications capability's schema
// migrations, applied with golang-migrate (see the Makefile), with their
// versions in public.notifications_schema_migrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

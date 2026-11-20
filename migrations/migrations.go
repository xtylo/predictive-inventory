// Package migrations embeds the SQL migration files into the binary.
//
// go:embed copies the files at compile time, so the service image carries
// its own schema and needs no extra files or tools to migrate the database.
package migrations

import "embed"

// FS holds every *.sql file in this folder, at the root of the file system.
//
//go:embed *.sql
var FS embed.FS

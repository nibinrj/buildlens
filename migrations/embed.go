// Package migrations embeds the SQL migration files into the binary.
//
// It lives next to the .sql files because //go:embed cannot reach a parent directory.
package migrations

import "embed"

// FS holds every migration file, applied in order by goose.
//
//go:embed *.sql
var FS embed.FS

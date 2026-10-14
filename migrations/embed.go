package migrations

import "embed"

// FS holds every migration, named NNNN_name.sql, at its root.
// Read it with migrate.Load / migrate.Up.
//
//go:embed *.sql
var FS embed.FS

package migrations

import "embed"

// FS contains the embedded SQL migrations.
//
//go:embed *.sql
var FS embed.FS

package migrations

import "embed"

// Files contains the SQL migrations embedded into the backend binary.
//
//go:embed *.sql
var Files embed.FS

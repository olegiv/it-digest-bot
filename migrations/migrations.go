// Package migrations ships the SQL schema migrations as an embedded FS so
// the binary is fully self-contained.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

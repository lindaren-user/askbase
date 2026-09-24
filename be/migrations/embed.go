// Package migrations exposes the versioned SQL migrations embedded in the migration binary.
package migrations

import "embed"

// Files contains every forward migration shipped with this AskBase version.
//
//go:embed *.up.sql
var Files embed.FS

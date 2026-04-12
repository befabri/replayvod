// Package migrations embeds the SQL migration files into the server binary
// so the runtime does not depend on a filesystem path.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed postgres/*.sql sqlite/*.sql
var all embed.FS

// Postgres returns the embedded PostgreSQL migrations filesystem.
func Postgres() fs.FS {
	sub, err := fs.Sub(all, "postgres")
	if err != nil {
		panic(err)
	}
	return sub
}

// SQLite returns the embedded SQLite migrations filesystem.
func SQLite() fs.FS {
	sub, err := fs.Sub(all, "sqlite")
	if err != nil {
		panic(err)
	}
	return sub
}

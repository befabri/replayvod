package main

import (
	"encoding/json"
	"io"

	"github.com/befabri/replayvod/server/migrations"
)

func writeMigrationManifest(output io.Writer) error {
	manifest, err := migrations.Manifest()
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(manifest)
}

package upgrade

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/migrations"
)

func compareCandidateMigrations(data []byte, expected map[string]string) error {
	var actual map[string]string
	if err := json.Unmarshal(data, &actual); err != nil {
		return fmt.Errorf("invalid candidate migration manifest: %w", err)
	}
	var differences []string
	for name, checksum := range expected {
		if value, ok := actual[name]; !ok {
			differences = append(differences, "missing: "+name)
		} else if value != checksum {
			differences = append(differences, "changed: "+name)
		}
	}
	for name := range actual {
		if _, ok := expected[name]; !ok {
			differences = append(differences, "unexpected: "+name)
		}
	}
	if len(differences) != 0 {
		sort.Strings(differences)
		return fmt.Errorf("candidate migration provenance differs from the test build:\n%s", strings.Join(differences, "\n"))
	}
	return nil
}

func TestCandidateMigrationProvenance(t *testing.T) {
	expected, err := migrations.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"matching", "changed up", "changed down", "missing", "unexpected", "empty", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			actual := maps.Clone(expected)
			want := ""
			switch kind {
			case "changed up", "changed down":
				direction := strings.TrimPrefix(kind, "changed ")
				path := "postgres/001_users." + direction + ".sql"
				actual[path] = strings.Repeat("0", 64)
				want = "changed: " + path
			case "missing":
				delete(actual, "sqlite/001_users.up.sql")
				want = "missing: sqlite/001_users.up.sql"
			case "unexpected":
				actual["sqlite/999_unexpected.up.sql"] = strings.Repeat("0", 64)
				want = "unexpected: sqlite/999_unexpected.up.sql"
			case "empty":
				actual = nil
				want = "missing:"
			case "malformed":
				want = "invalid candidate migration manifest"
			}
			data, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "malformed" {
				data = []byte("server starting")
			}
			err = compareCandidateMigrations(data, expected)
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

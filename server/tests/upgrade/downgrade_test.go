//go:build upgrade

package upgrade

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

func (h *installation) downgrade(candidate, previous string, b baseline, original snapshot, p projection) {
	// Project the baseline schema so newer tables and columns have explicit loss semantics.
	retained := h.projectSnapshot(original, "before-downgrade", p)
	h.stop()
	h.maintenance(candidate, "rollback")
	files, err := fs.Glob(embeddedMigrations()[h.backend], "*.up.sql")
	if err != nil {
		h.t.Fatal(err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	var statements []string
	for _, file := range files {
		if _, released := b.MigrationSHA256["server/migrations/"+h.backend+"/"+filepath.Base(file)]; released {
			continue
		}
		down, err := fs.ReadFile(embeddedMigrations()[h.backend], strings.TrimSuffix(file, ".up.sql")+".down.sql")
		if err != nil {
			h.t.Fatal(err)
		}
		version := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		statements = append(statements, string(down), "DELETE FROM schema_migrations WHERE version='"+strings.ReplaceAll(version, "'", "''")+"'")
	}
	// The application is stopped; down SQL and ledger removal commit together.
	h.probe(probeRequest{Exec: statements})
	docker(h.t, "stop", "--time", "1", h.app)
	h.start(previous, "downgraded")
	actual := h.takeSnapshot(original, "downgraded")
	// The rules hold on the way back too; a dropped table's rows are not expected back.
	if err := p.compareDown(withoutLedger(retained), withoutLedger(actual)); err != nil {
		h.t.Fatalf("downgrade lost retained data: %v", err)
	}
	h.assertLedger(b.MigrationSHA256, false)
	h.assertOldLedger(original, actual)
	h.checkIntegrity()
	h.seedServed()
	if b.hasMigration(h.backend, "044_user_states") {
		h.verifyPlayback(150.25)
	}
	retained = h.takeSnapshot(original, "before-reupgrade")
	h.stop()
	h.start(candidate, "reupgraded")
	actual = h.projectSnapshot(original, "reupgraded", p)
	if err := p.compare(withoutLedger(retained), withoutLedger(actual)); err != nil {
		h.t.Fatalf("reupgrade lost retained data: %v", err)
	}
	h.assertLedger(b.MigrationSHA256, true)
	h.assertOldLedger(original, actual)
	h.checkIntegrity()
	h.verifyPreserved(150.25)
	rows := h.probe(probeRequest{Queries: map[string]string{
		"invites": "SELECT * FROM invites", "schedule_requests": "SELECT * FROM schedule_requests",
	}})
	for table, values := range rows {
		if _, existed := original[table]; !existed && len(values) != 0 {
			h.t.Fatalf("downgrade retained unsupported new state in %s: %v", table, values)
		}
	}
}

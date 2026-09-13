package upgrade

import (
	"fmt"
	"io/fs"
	"maps"
	"sort"
	"strings"

	"github.com/befabri/replayvod/server/migrations"
)

// declarationSite points failures at the rule file.
const declarationSite = "server/tests/upgrade/expectations_test.go"

// transformations declares how the migrations a candidate adds may change
// historical tables, keyed by migration version and historical table name. A
// rule applies only to baselines that lack the migration; remove it once every
// retained baseline contains the migration.
//
//	"047_split_titles": {
//		"video_titles":    {renamedTo: "recording_titles", columns: map[string]string{"title_id": "id"}},
//		"legacy_requests": {dropped: true},
//		"event_logs":      grows(),
//	},
var transformations = map[string]map[string]tableRule{
	"047_videos_deletion_kind_missing": {
		"videos": {
			up: func(rows []map[string]any, _ snapshot) {
				for _, row := range rows {
					if row["deleted_at"] != nil && row["deletion_kind"] != "missing" {
						row["thumbnail"] = nil
					}
				}
			},
			down: func(rows []map[string]any, _ snapshot) {
				for _, row := range rows {
					if row["deletion_kind"] == "missing" {
						row["deletion_kind"] = "manual"
					}
				}
			},
		},
	},
	"049_videos_failed_truncated": {
		"videos": {up: func(rows []map[string]any, historical snapshot) {
			saved := map[any]bool{}
			for _, part := range historical["video_parts"] {
				if size, ok := part["size_bytes"].(float64); ok && size > 0 {
					saved[part["video_id"]] = true
				}
			}
			for _, row := range rows {
				if row["status"] == "FAILED" && row["deleted_at"] == nil && !saved[row["id"]] {
					// The JSON probe exposes PG booleans and SQLite integers.
					if _, ok := row["truncated"].(bool); ok {
						row["truncated"] = false
					} else {
						row["truncated"] = float64(0)
					}
				}
			}
		}},
	},
	"051_recording_quality": {
		"videos":             {down: restoreLegacyQuality},
		"download_schedules": {down: restoreLegacyQuality},
	},
	"057_execution_ownership": {
		"videos": {up: failOrphanAdmissions},
	},
	"060_recording_workflow_upgrade": {
		"video_parts": grows(),
		"jobs":        {up: upgradeRecordingCheckpoints, compare: compareCheckpointRows},
	},
	"062_canonical_recording_timeline": {
		"video_metadata_changes": grows(),
	},
}

func restoreLegacyQuality(rows []map[string]any, _ snapshot) {
	for _, row := range rows {
		if row["quality"] == "1440" || row["quality"] == "BEST" {
			row["quality"] = "HIGH"
		}
	}
}

// tableRule describes one intentional change to a table.
type tableRule struct {
	dropped   bool
	renamedTo string
	columns   map[string]string                          // historical column name to its new name
	compare   func(before, after []map[string]any) error // replaces the exact row comparison
	// Value transformations edit a copy of the expected rows, never actual
	// database rows. All other values and row counts remain exact assertions.
	// The snapshot supplies historical values from related tables when needed.
	up, down func(rows []map[string]any, historical snapshot)
}

func (r tableRule) structural() bool {
	return r.dropped || r.renamedTo != "" || len(r.columns) != 0 || r.compare != nil
}

type valueChange struct {
	table string
	rule  tableRule
}

// grows requires every historical row to remain while accepting new rows.
func grows() tableRule { return tableRule{compare: retainsRows} }

func retainsRows(before, after []map[string]any) error {
	remaining := map[string]int{}
	for _, row := range canonicalRows(after) {
		remaining[row]++
	}
	for _, row := range canonicalRows(before) {
		if remaining[row] == 0 {
			return fmt.Errorf("historical row missing: %s", row)
		}
		remaining[row]--
	}
	return nil
}

// schema lists the column names of each historical table, captured from the
// catalog so empty tables keep their names.
type schema map[string][]string

// A projection reads the candidate through the historical schema of one upgrade case.
type projection struct {
	added   []string             // up migrations the candidate applies to the baseline
	rules   map[string]tableRule // by historical table name
	columns schema               // historical columns, set once the baseline is read
	changes []valueChange        // value transformations in migration order
}

// projectionFor selects the rules of the migrations the candidate adds to b.
func projectionFor(rules map[string]map[string]tableRule, candidate fs.FS, backend string, b baseline) (projection, error) {
	files, err := fs.Glob(candidate, "*.up.sql")
	if err != nil {
		return projection{}, err
	}
	p := projection{rules: map[string]tableRule{}}
	owner := map[string]string{}
	for _, file := range files {
		version := strings.TrimSuffix(file, ".up.sql")
		if b.hasMigration(backend, version) {
			continue
		}
		p.added = append(p.added, version)
		for _, table := range sortedRuleTables(rules[version]) {
			rule := rules[version][table]
			if rule.structural() {
				if previous, declared := owner[table]; declared {
					return projection{}, fmt.Errorf("%s: %s is declared by both %s and %s; declare the combined structural change once", declarationSite, table, previous, version)
				}
				owner[table] = version
				p.rules[table] = rule
			}
			if rule.up != nil || rule.down != nil {
				p.changes = append(p.changes, valueChange{table: table, rule: rule})
			}
		}
	}
	sort.Strings(p.added)
	return p, nil
}

func sortedRuleTables(rules map[string]tableRule) []string {
	tables := make([]string, 0, len(rules))
	for table := range rules {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

// queries reads the historical tables through the candidate schema, aliased back
// to their historical names so the rows compare directly.
func (p projection) queries(previous snapshot) map[string]string {
	queries := map[string]string{}
	for table, rows := range previous {
		rule := p.rules[table]
		if rule.dropped {
			continue
		}
		names := p.columns[table]
		if len(names) == 0 && len(rows) > 0 {
			for column := range rows[0] {
				names = append(names, column)
			}
		}
		columns := []string{"*"}
		if len(names) > 0 {
			columns = columns[:0]
			for _, column := range names {
				selected := quoteIdentifier(column)
				if renamed, ok := rule.columns[column]; ok {
					selected = quoteIdentifier(renamed) + " AS " + quoteIdentifier(column)
				}
				columns = append(columns, selected)
			}
			sort.Strings(columns)
		}
		source := table
		if rule.renamedTo != "" {
			source = rule.renamedTo
		}
		queries[table] = "SELECT " + strings.Join(columns, ",") + " FROM " + quoteIdentifier(source)
	}
	return queries
}

// compare checks the candidate rows against the historical rows under the rules.
func (p projection) compare(before, after snapshot) error {
	return p.compareDirection(before, after, false)
}

func (p projection) compareDown(before, after snapshot) error {
	return p.compareDirection(before, after, true)
}

func (p projection) compareDirection(before, after snapshot, down bool) error {
	expected := cloneSnapshot(before)
	for i := range p.changes {
		change := p.changes[i]
		transform := change.rule.up
		if down {
			change = p.changes[len(p.changes)-1-i]
			transform = change.rule.down
		}
		if transform != nil {
			transform(expected[change.table], before)
		}
	}
	return p.compareExpected(expected, after)
}

func cloneSnapshot(s snapshot) snapshot {
	cloned := snapshot{}
	for table, rows := range s {
		for _, row := range rows {
			cloned[table] = append(cloned[table], maps.Clone(row))
		}
		if len(rows) == 0 {
			cloned[table] = nil
		}
	}
	return cloned
}

func (p projection) compareExpected(before, after snapshot) error {
	for _, table := range sortedTables(before) {
		rule := p.rules[table]
		if rule.dropped {
			continue
		}
		actual, exists := after[table]
		if !exists {
			return p.explain(fmt.Errorf("table %s is missing", table))
		}
		if rule.compare != nil {
			if err := rule.compare(before[table], actual); err != nil {
				return fmt.Errorf("%s: %w", table, err)
			}
			continue
		}
		if err := compareRows(table, before[table], actual); err != nil {
			return p.explain(err)
		}
	}
	return nil
}

// explain turns an undeclared difference into an instruction.
func (p projection) explain(err error) error {
	if len(p.added) == 0 {
		return fmt.Errorf("%w; the candidate adds no migration, so it changed historical data at startup", err)
	}
	return fmt.Errorf("%w; if %s changes this intentionally, declare the transformation in %s", err, strings.Join(p.added, ", "), declarationSite)
}

// validateTransformations rejects rules that can never apply.
func validateTransformations(rules map[string]map[string]tableRule, candidates map[string]fs.FS, m manifest) error {
	for version, tables := range rules {
		known, live := false, false
		for backend, files := range candidates {
			if _, err := fs.Stat(files, version+".up.sql"); err != nil {
				continue
			}
			known = true
			for _, b := range m.Baselines {
				if !b.hasMigration(backend, version) {
					live = true
				}
			}
		}
		if !known {
			return fmt.Errorf("%s: %s is not an embedded migration", declarationSite, version)
		}
		if !live {
			return fmt.Errorf("%s: every retained baseline contains %s; remove its rules", declarationSite, version)
		}
		for table, rule := range tables {
			if rule.dropped && (rule.renamedTo != "" || len(rule.columns) != 0 || rule.compare != nil || rule.up != nil || rule.down != nil) {
				return fmt.Errorf("%s: %s.%s is dropped and cannot declare other changes", declarationSite, version, table)
			}
		}
	}
	return nil
}

func quoteIdentifier(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// embeddedMigrations returns the test binary's embedded SQL. The candidate
// image is verified to embed the same files, so no checkout files are read.
func embeddedMigrations() map[string]fs.FS {
	return map[string]fs.FS{"postgres": migrations.Postgres(), "sqlite": migrations.SQLite()}
}

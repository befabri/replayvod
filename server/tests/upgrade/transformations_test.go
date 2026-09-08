package upgrade

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func fakeMigrations(versions ...string) fstest.MapFS {
	files := fstest.MapFS{}
	for _, version := range versions {
		files[version+".up.sql"] = &fstest.MapFile{}
		files[version+".down.sql"] = &fstest.MapFile{}
	}
	return files
}

// released builds a baseline whose provenance lists the versions for both backends.
func released(versions ...string) baseline {
	b := baseline{MigrationSHA256: map[string]string{}}
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, version := range versions {
			b.MigrationSHA256["server/migrations/"+backend+"/"+version+".up.sql"] = strings.Repeat("a", 64)
		}
	}
	return b
}

func TestDeclaredTransformationsApply(t *testing.T) {
	m, err := readManifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTransformations(transformations, embeddedMigrations(), m); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionUsesRulesOfAddedMigrationsOnly(t *testing.T) {
	rules := map[string]map[string]tableRule{
		"002_second": {"history": grows()},
		"003_third":  {"legacy": {dropped: true}},
	}
	candidate := fakeMigrations("001_first", "002_second", "003_third")
	p, err := projectionFor(rules, candidate, "sqlite", released("001_first", "002_second"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.added, ",") != "003_third" || len(p.rules) != 1 || !p.rules["legacy"].dropped {
		t.Fatalf("projection = %+v", p)
	}
	p, err = projectionFor(rules, candidate, "sqlite", released("001_first"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.added, ",") != "002_second,003_third" || len(p.rules) != 2 {
		t.Fatalf("projection = %+v", p)
	}
	if _, err := projectionFor(rules, candidate, "postgres", released("001_first")); err != nil {
		t.Fatal(err)
	}
	rules["003_third"]["history"] = grows()
	if _, err := projectionFor(rules, candidate, "sqlite", released()); err == nil || !strings.Contains(err.Error(), declarationSite) {
		t.Fatalf("conflicting rules accepted: %v", err)
	}
}

func TestProjectionQueriesFollowRules(t *testing.T) {
	p := projection{rules: map[string]tableRule{
		"titles":  {renamedTo: "recording_titles", columns: map[string]string{"name": "label"}},
		"legacy":  {dropped: true},
		"history": grows(),
		"drafts":  {columns: map[string]string{"body": "text"}},
	}, columns: schema{"drafts": {"id", "body"}, "users": {"id", "login"}}}
	previous := snapshot{
		"titles":  {{"id": 1, "name": "x"}},
		"legacy":  {{"id": 1}},
		"history": {},
		"drafts":  {},
		"users":   {{"id": "1", "login": "a"}},
	}
	// drafts held no rows, so only the captured schema can alias its renamed column.
	want := map[string]string{
		"titles":  `SELECT "id","label" AS "name" FROM "recording_titles"`,
		"history": `SELECT * FROM "history"`,
		"drafts":  `SELECT "id","text" AS "body" FROM "drafts"`,
		"users":   `SELECT "id","login" FROM "users"`,
	}
	got := p.queries(previous)
	if len(got) != len(want) {
		t.Fatalf("queries = %v", got)
	}
	for table, query := range want {
		if got[table] != query {
			t.Fatalf("%s query = %q, want %q", table, got[table], query)
		}
	}
}

func TestProjectionCompareNamesDeclarationSite(t *testing.T) {
	before := snapshot{"history": {{"id": 1}, {"id": 2}}}
	grown := snapshot{"history": {{"id": 1}, {"id": 2}, {"id": 3}}}
	shrunk := snapshot{"history": {{"id": 1}}}
	for _, tc := range []struct {
		name  string
		p     projection
		after snapshot
		want  string
	}{
		{"exact rows pass", projection{added: []string{"047_x"}}, before, ""},
		{"undeclared growth names the migration", projection{added: []string{"047_x"}}, grown, "047_x changes this intentionally, declare the transformation in " + declarationSite},
		{"growth without migration blames startup", projection{}, grown, "changed historical data at startup"},
		{"declared growth passes", projection{added: []string{"047_x"}, rules: map[string]tableRule{"history": grows()}}, grown, ""},
		{"declared growth still requires historical rows", projection{added: []string{"047_x"}, rules: map[string]tableRule{"history": grows()}}, shrunk, "history: historical row missing"},
		{"dropped table is skipped", projection{added: []string{"047_x"}, rules: map[string]tableRule{"history": {dropped: true}}}, snapshot{}, ""},
		{"missing table without rule", projection{added: []string{"047_x"}}, snapshot{}, "table history is missing; if 047_x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.compare(before, tc.after)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("compare = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateTransformations(t *testing.T) {
	candidates := map[string]fs.FS{"sqlite": fakeMigrations("001_first", "002_second"), "postgres": fakeMigrations("001_first", "002_second", "003_pg_only")}
	m := manifest{Baselines: []baseline{released("001_first"), released("001_first", "002_second", "003_pg_only")}}
	for _, tc := range []struct {
		name  string
		rules map[string]map[string]tableRule
		want  string
	}{
		{"live rule", map[string]map[string]tableRule{"002_second": {"t": grows()}}, ""},
		{"unknown migration", map[string]map[string]tableRule{"099_missing": {"t": grows()}}, "not an embedded migration"},
		{"dead rule", map[string]map[string]tableRule{"001_first": {"t": grows()}}, "every retained baseline contains 001_first"},
		{"backend specific rule is live where a baseline lacks it", map[string]map[string]tableRule{"003_pg_only": {"t": grows()}}, ""},
		{"backend specific rule is dead once every baseline has it", nil, "every retained baseline contains 003_pg_only"},
		{"dropped with rename", map[string]map[string]tableRule{"002_second": {"t": {dropped: true, renamedTo: "u"}}}, "dropped and cannot declare other changes"},
		{"dropped with up values", map[string]map[string]tableRule{"002_second": {"t": {dropped: true, up: restoreLegacyQuality}}}, "dropped and cannot declare other changes"},
		{"dropped with down values", map[string]map[string]tableRule{"002_second": {"t": {dropped: true, down: restoreLegacyQuality}}}, "dropped and cannot declare other changes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules, baselines := tc.rules, m
			if rules == nil {
				rules = map[string]map[string]tableRule{"003_pg_only": {"t": grows()}}
				baselines = manifest{Baselines: []baseline{released("001_first", "003_pg_only"), m.Baselines[1]}}
			}
			err := validateTransformations(rules, candidates, baselines)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("validate = %v, want %q", err, tc.want)
			}
		})
	}
	if err := validateTransformations(nil, candidates, m); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationValueTransformations(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			var yes, no any = true, false
			if backend == "sqlite" {
				yes, no = float64(1), float64(0)
			}
			before := snapshot{
				"videos": {
					{"id": float64(1), "status": "DONE", "deleted_at": "old", "deletion_kind": "retention", "thumbnail": "purged", "truncated": no, "quality": "LOW", "title": "keep"},
					{"id": float64(2), "status": "DONE", "deleted_at": nil, "deletion_kind": nil, "thumbnail": "live", "truncated": yes, "quality": "MEDIUM", "title": "keep"},
					{"id": float64(3), "status": "FAILED", "deleted_at": nil, "thumbnail": nil, "truncated": yes, "quality": "HIGH"},
					{"id": float64(4), "status": "FAILED", "deleted_at": nil, "thumbnail": nil, "truncated": yes, "quality": "HIGH"},
					{"id": float64(5), "status": "FAILED", "deleted_at": nil, "thumbnail": nil, "truncated": yes, "quality": "HIGH"},
					{"id": float64(6), "status": "FAILED", "deleted_at": "old", "thumbnail": nil, "truncated": yes, "quality": "HIGH"},
				},
				"video_parts":        {{"video_id": float64(4), "size_bytes": float64(0)}, {"video_id": float64(5), "size_bytes": float64(42)}},
				"download_schedules": {{"id": float64(1), "quality": "LOW"}, {"id": float64(2), "quality": "MEDIUM"}},
				"unrelated":          {{"note": "é日本語", "value": nil}},
			}
			original := cloneSnapshot(before)
			files := fakeMigrations("047_videos_deletion_kind_missing", "049_videos_failed_truncated", "051_recording_quality")
			p, err := projectionFor(transformations, files, backend, released())
			if err != nil {
				t.Fatal(err)
			}
			after := cloneSnapshot(before)
			after["videos"][0]["thumbnail"] = nil
			after["videos"][2]["truncated"] = no
			after["videos"][3]["truncated"] = no
			if err := p.compare(before, after); err != nil {
				t.Fatal(err)
			}
			if err := compareSnapshots(original, before); err != nil {
				t.Fatalf("comparison mutated historical evidence: %v", err)
			}
			for name, corrupt := range map[string]func(snapshot){
				"missing thumbnail cleanup":    func(s snapshot) { s["videos"][0]["thumbnail"] = "purged" },
				"live thumbnail erased":        func(s snapshot) { s["videos"][1]["thumbnail"] = nil },
				"empty failure unchanged":      func(s snapshot) { s["videos"][2]["truncated"] = yes },
				"stub failure unchanged":       func(s snapshot) { s["videos"][3]["truncated"] = yes },
				"saved media reclassified":     func(s snapshot) { s["videos"][4]["truncated"] = no },
				"deleted failure reclassified": func(s snapshot) { s["videos"][5]["truncated"] = no },
				"unrelated video column":       func(s snapshot) { s["videos"][0]["title"] = "lost" },
				"missing video":                func(s snapshot) { s["videos"] = s["videos"][1:] },
				"duplicate video":              func(s snapshot) { s["videos"] = append(s["videos"], s["videos"][0]) },
				"changed dependent row":        func(s snapshot) { s["video_parts"][1]["size_bytes"] = float64(0) },
			} {
				t.Run(name, func(t *testing.T) {
					broken := cloneSnapshot(after)
					corrupt(broken)
					if err := p.compare(before, broken); err == nil {
						t.Fatal("undeclared data change accepted")
					}
				})
			}

			// Newly written missing tombstones may keep a poster. Down must not
			// rerun the old up backfill; it only maps unsupported enum values.
			written := cloneSnapshot(after)
			written["videos"][0]["deletion_kind"] = "missing"
			written["videos"][0]["thumbnail"] = "new-retained-poster"
			written["videos"][2]["quality"] = "BEST"
			written["download_schedules"][1]["quality"] = "1440"
			down := cloneSnapshot(written)
			down["videos"][0]["deletion_kind"] = "manual"
			down["videos"][2]["quality"] = "HIGH"
			down["download_schedules"][1]["quality"] = "HIGH"
			if err := p.compareDown(written, down); err != nil {
				t.Fatal(err)
			}
			broken := cloneSnapshot(down)
			broken["videos"][0]["quality"] = "HIGH"
			if err := p.compareDown(written, broken); err == nil {
				t.Fatal("rollback lost legacy LOW quality without failing")
			}
			if err := p.compareDown(written, written); err == nil {
				t.Fatal("rollback accepted unconverted new quality and deletion values")
			}

			// A release containing 047 must not get its allowance merely because
			// 049 also changes videos. Select each value rule independently.
			partial, err := projectionFor(transformations, files, backend, released("047_videos_deletion_kind_missing"))
			if err != nil {
				t.Fatal(err)
			}
			partialWant := cloneSnapshot(after)
			partialWant["videos"][0]["thumbnail"] = "purged"
			if err := partial.compare(before, partialWant); err != nil {
				t.Fatal(err)
			}
			if err := partial.compare(before, after); err == nil {
				t.Fatal("already-applied backfill was allowed to change historical data again")
			}
		})
	}
}

func TestValueTransformationsFollowMigrationOrderInBothDirections(t *testing.T) {
	rules := map[string]map[string]tableRule{
		"001_add": {"t": {
			up:   func(rows []map[string]any, _ snapshot) { rows[0]["v"] = rows[0]["v"].(int) + 1 },
			down: func(rows []map[string]any, _ snapshot) { rows[0]["v"] = rows[0]["v"].(int) - 1 },
		}},
		"002_double": {"t": {
			up:   func(rows []map[string]any, _ snapshot) { rows[0]["v"] = rows[0]["v"].(int) * 2 },
			down: func(rows []map[string]any, _ snapshot) { rows[0]["v"] = rows[0]["v"].(int) / 2 },
		}},
	}
	p, err := projectionFor(rules, fakeMigrations("002_double", "001_add"), "sqlite", released())
	if err != nil {
		t.Fatal(err)
	}
	before, after := snapshot{"t": {{"v": 2}}}, snapshot{"t": {{"v": 6}}}
	if err := p.compare(before, after); err != nil {
		t.Fatal(err)
	}
	if err := p.compareDown(after, before); err != nil {
		t.Fatal(err)
	}
}

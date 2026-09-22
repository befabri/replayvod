package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestQueryRowShapes(t *testing.T) {
	for _, d := range []dialect{
		{name: "pg", genPkg: "pggen", adapterType: "PGAdapter"},
		{name: "sqlite", genPkg: "sqlitegen", adapterType: "SQLiteAdapter"},
	} {
		for _, query := range []struct{ name, mapper string }{
			{"ListVideosForStorageScan", "StorageScanVideos"},
			{"ListMissingTombstones", "MissingTombstones"},
			{"ListVideosForStorageWitness", "StorageWitnesses"},
		} {
			t.Run(d.name+"/"+query.name, func(t *testing.T) {
				r := renderer{
					cfg: testConfig(),
					d:   d,
					domain: map[string]map[string]string{"StorageScanVideo": {
						"VideoID": "int64", "Filename": "string", "Status": "string",
					}},
					gen: map[string]map[string]string{query.name + "Row": {
						"VideoID": "int64", "Filename": "string", "Status": "string",
					}},
					queries: map[string]querySig{query.name: {results: []string{"[]" + query.name + "Row", "error"}}},
				}
				hand := fmt.Sprintf(`func (a *%s) %s(ctx context.Context) ([]repository.StorageScanVideo, error) {
	rows, err := a.queries.%s(ctx)
	if err != nil { return nil, err }
	out := make([]repository.StorageScanVideo, len(rows))
	for i, row := range rows {
		out[i] = repository.StorageScanVideo{VideoID: row.VideoID, Filename: row.Filename, Status: row.Status}
	}
	return out, nil
}`, d.adapterType, query.name, query.name)
				sig := ctxSig([]string{"[]StorageScanVideo", "error"})
				c := harvested(t, r, query.name, sig, hand)
				if c == nil || !strings.Contains(c.emit, "return "+d.name+query.mapper+"ToDomain(rows), nil") {
					t.Fatalf("projection was not harvested with its own mapper: %+v", c)
				}
				for _, changed := range []string{
					strings.Replace(hand, "Filename: row.Filename", "Filename: row.Status", 1),
					strings.Replace(hand, "Status: row.Status", `Status: "DONE"`, 1),
					strings.Replace(hand, "Status: row.Status", "", 1),
				} {
					if harvested(t, r, query.name, sig, changed) != nil {
						t.Fatal("projection with different field semantics was harvested")
					}
				}
				r.queries[query.name] = querySig{results: []string{"[]UnknownRow", "error"}}
				if len(r.candidates(query.name, sig)) != 0 {
					t.Fatal("unregistered row type used another projection's mapper")
				}
			})
		}
	}
}

func TestQueryMapperFields(t *testing.T) {
	spec := genSpec{name: "RetentionVideo", row: "ListRetentionCandidatesRow"}
	domain := map[string]map[string]string{"RetentionVideo": {
		"VideoID": "int64", "BroadcasterID": "string", "DownloadedAt": "*time.Time", "RetentionWindowHours": "*int64",
	}}
	for _, tc := range []struct {
		name, timeType, hoursType, timeExpr, hoursExpr string
	}{
		{"pg", "*time.Time", "*int32", "src.DownloadedAt", "int32PtrToInt64Ptr(src.RetentionWindowHours)"},
		{"sqlite", "*sqlitetype.Time", "sql.NullInt64", "timePtrFromSQLite(src.DownloadedAt)", "fromNullInt64(src.RetentionWindowHours)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{"VideoId": "int64", "BroadcasterID": "string", "DownloadedAt": tc.timeType, "RetentionWindowHours": tc.hoursType}
			rows := map[string]map[string]string{spec.rowType(): fields}
			got, err := (renderer{cfg: testConfig(), domain: domain, gen: rows}).mapperLiteral(spec, "src")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"VideoID: src.VideoId", "DownloadedAt: " + tc.timeExpr, "RetentionWindowHours: " + tc.hoursExpr} {
				if !strings.Contains(got, want) {
					t.Fatalf("mapper missing %q:\n%s", want, got)
				}
			}
			delete(fields, "VideoId")
			fields["ID"] = "int64"
			if _, err := (renderer{cfg: testConfig(), domain: domain, gen: rows}).mapperLiteral(spec, "src"); err == nil || !strings.Contains(err.Error(), "alias the SQL column") {
				t.Fatalf("unaliased ID should fail: %v", err)
			}
			fields["VideoID"] = "[]byte"
			if _, err := (renderer{cfg: testConfig(), domain: domain, gen: rows}).mapperLiteral(spec, "src"); err == nil || !strings.Contains(err.Error(), "no conversion rule") {
				t.Fatalf("unknown field conversion should fail: %v", err)
			}
		})
	}
}

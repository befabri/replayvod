package upgrade

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

type snapshot map[string][]map[string]any

// compareSnapshots requires every table in before to hold exactly the same rows in after.
func compareSnapshots(before, after snapshot) error {
	for _, table := range sortedTables(before) {
		actual, exists := after[table]
		if !exists {
			return fmt.Errorf("table %s is missing", table)
		}
		if err := compareRows(table, before[table], actual); err != nil {
			return err
		}
	}
	return nil
}

func compareRows(table string, want, got []map[string]any) error {
	wantRows, gotRows := canonicalRows(want), canonicalRows(got)
	if reflect.DeepEqual(wantRows, gotRows) {
		return nil
	}
	for i := 0; i < min(len(wantRows), len(gotRows)); i++ {
		if wantRows[i] != gotRows[i] {
			return fmt.Errorf("%s differs: expected row %s; got %s", table, wantRows[i], gotRows[i])
		}
	}
	return fmt.Errorf("%s row count changed: %d to %d", table, len(wantRows), len(gotRows))
}

func sortedTables(s snapshot) []string {
	tables := make([]string, 0, len(s))
	for table := range s {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

func canonicalRows(rows []map[string]any) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		data, _ := json.Marshal(row)
		out[i] = string(data)
	}
	sort.Strings(out)
	return out
}

func TestSnapshotsDetectDataLoss(t *testing.T) {
	original := snapshot{"history": {{"id": 1, "note": nil}, {"id": 2, "note": "é日本語"}}}
	for _, tc := range []struct {
		name      string
		after     snapshot
		wantError bool
	}{
		{"different row order", snapshot{"history": {{"id": 2, "note": "é日本語"}, {"id": 1, "note": nil}}}, false},
		{"deleted table", snapshot{}, true},
		{"deleted row", snapshot{"history": {{"id": 1, "note": nil}}}, true},
		{"changed value", snapshot{"history": {{"id": 1, "note": ""}, {"id": 2, "note": "é日本語"}}}, true},
		{"duplicated row", snapshot{"history": {{"id": 1, "note": nil}, {"id": 1, "note": nil}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := compareSnapshots(original, tc.after); (err != nil) != tc.wantError {
				t.Fatalf("compare = %v, want error %t", err, tc.wantError)
			}
		})
	}
}

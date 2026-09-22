package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	testConfigOnce sync.Once
	testConfigVal  *config
)

// testConfig loads the project's real config so shape tests render the
// conventions the adapters use.
func testConfig() *config {
	testConfigOnce.Do(func() {
		c, err := loadConfig("../../repo-adapter-gen.yaml")
		if err != nil {
			panic(err)
		}
		testConfigVal = c
	})
	return testConfigVal
}

func TestProjectConfig(t *testing.T) {
	c := testConfig()
	if len(c.Dialects) != 2 || !c.denied("WithTx") || c.alias("ListEventSubSnapshots") != "ListSnapshots" || c.alias("GetUser") != "GetUser" {
		t.Fatalf("unexpected project config: %+v", c)
	}
	if got, ok := c.convertArg("int", "int32", "limit"); !ok || got != "int32(limit)" {
		t.Fatalf("argument rule = %q, %v", got, ok)
	}
	if got, ok := c.conversion("sqlitetype.Time", "time.Time"); !ok || got != "%s.Time" {
		t.Fatalf("conversion rule = %q, %v", got, ok)
	}
	if spec, ok := c.mapperSpec("StorageScanVideo", "ListMissingTombstonesRow"); !ok || spec.name != "MissingTombstone" {
		t.Fatalf("mapper spec = %+v, %v", spec, ok)
	}
}

func TestConfigValidation(t *testing.T) {
	src, err := os.ReadFile("../../repo-adapter-gen.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(string) string{
		"unknown field":        func(s string) string { return s + "\nunknown: 1\n" },
		"missing error mapper": func(s string) string { return strings.Replace(s, "error_mapper: mapErr", `error_mapper: ""`, 1) },
		"format without %w": func(s string) string {
			return strings.Replace(s, `error_format: "{dialect} {action}: %w"`, `error_format: "{dialect} {action}: %v"`, 1)
		},
		"duplicate rule": func(s string) string {
			return strings.Replace(s, "conversions:\n", "conversions:\n  - {from: \"int64\", to: \"int64\", expr: \"%s\"}\n", 1)
		},
		"unknown locking dialect": func(s string) string { return strings.Replace(s, "locking_dialect: pg", "locking_dialect: mysql", 1) },
		"unparsable equivalent": func(s string) string {
			return strings.Replace(s, `literal: "sql.NullInt64{Int64: %s, Valid: true}"`, `literal: "sql.NullInt64{%s"`, 1)
		},
	} {
		path := filepath.Join(t.TempDir(), "repo-adapter-gen.yaml")
		edited := edit(string(src))
		if edited == string(src) {
			t.Fatalf("%s: edit did not apply", name)
		}
		if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil {
			t.Errorf("%s: config loaded", name)
		}
	}
}

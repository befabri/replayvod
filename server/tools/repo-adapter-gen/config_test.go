package main

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// TestEngineReadsConventionsOnlyFromConfig keeps the engine extractable: no
// name the config declares may appear in the engine's code, so a convention
// cannot quietly move back from the YAML into a template. Comments are not
// scanned; test files may name the project freely.
func TestEngineReadsConventionsOnlyFromConfig(t *testing.T) {
	c := testConfig()
	builtin := map[string]bool{}
	for _, b := range strings.Fields("int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float32 float64 string bool byte rune error len make new append cap copy") {
		builtin[b] = true
	}
	idents := map[string]bool{c.Domain.Package: true}
	literals := map[string]bool{
		c.Domain.Package: true, c.Domain.Import: true, c.Domain.Models: true, c.Domain.InterfaceFile: true,
		c.Domain.Interface: true, c.Domain.ValueObjects: true, c.Conventions.QueriesField: true,
	}
	for _, d := range c.Dialects {
		for _, v := range []string{d.Adapter, d.GenPackage} {
			idents[v], literals[v] = true, true
		}
		for _, v := range []string{d.Name, d.Dir, d.GenImport} {
			literals[v] = true
		}
	}
	cv := c.Conventions
	for _, v := range []string{cv.ErrorMapper, cv.TransactionCheck, cv.Affected, cv.NotFound, cv.NoTransaction, cv.MapperSuffix} {
		for _, part := range strings.Split(v, ".") {
			idents[part], literals[part] = true, true
		}
	}
	call := regexp.MustCompile(`(^|[^.\w])([a-z]\w*)\(`)
	for _, rules := range [][]rule{c.Conversions, c.Arguments, c.Results} {
		for _, r := range rules {
			for _, m := range call.FindAllStringSubmatch(r.Expr, -1) {
				if !builtin[m[2]] {
					idents[m[2]], literals[m[2]] = true, true
				}
			}
		}
	}
	for _, e := range c.Equivalents {
		idents[e.Helper], literals[e.Helper] = true, true
	}
	for name, query := range c.Aliases {
		literals[name], literals[query] = true, true
	}
	for name := range c.Deny {
		literals[name] = true
	}
	for _, spec := range c.Types {
		literals[spec.Name] = true
	}
	words := make(map[string]*regexp.Regexp, len(literals))
	for w := range literals {
		words[w] = regexp.MustCompile(`(^|\W)` + regexp.QuoteMeta(w) + `(\W|$)`)
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		var s scanner.Scanner
		s.Init(fset.AddFile(file, fset.Base(), len(src)), src, nil, 0)
		for {
			pos, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			switch tok {
			case token.IDENT:
				if idents[lit] {
					t.Errorf("%s: identifier %s is a project convention; read it from the config", fset.Position(pos), lit)
				}
			case token.STRING:
				v, err := strconv.Unquote(lit)
				if err != nil {
					continue
				}
				for w, re := range words {
					if re.MatchString(v) {
						t.Errorf("%s: string %q names the project (%s); read it from the config", fset.Position(pos), v, w)
					}
				}
			}
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryValueObjects(t *testing.T) {
	values, err := valueObjects("../../internal/repository")
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range map[string]int{"BatchPage": 2, "BatchSize": 1} {
		if len(values[name]) != count {
			t.Fatalf("%s accessors = %+v", name, values[name])
		}
	}
}

func TestValueObjectDiscoveryRequiresPrivateFieldsAndValueValidation(t *testing.T) {
	dir := t.TempDir()
	source := `package repository
type Page struct { limit int }
func (p Page) Limit() int { return p.limit }
func (p Page) Validate() error { return nil }
type Public struct { Limit int }
func (p Public) Count() int { return p.Limit }
func (p Public) Validate() error { return nil }
type Unchecked struct { limit int }
func (p Unchecked) Limit() int { return p.limit }
type Pointer struct { limit int }
func (p Pointer) Limit() int { return p.limit }
func (p *Pointer) Validate() error { return nil }
type Embedded struct { Page }
func (p Embedded) Limit() int { return 1 }
func (p Embedded) Validate() error { return nil }
`
	if err := os.WriteFile(filepath.Join(dir, "page.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := valueObjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || len(values["Page"]) != 1 || values["Page"][0] != (param{"Limit", "int"}) {
		t.Fatalf("unexpected value objects: %+v", values)
	}
}

func TestValueObjectArguments(t *testing.T) {
	r := testRenderer()
	r.values = map[string][]param{
		"BatchPage": {{"AfterID", "int64"}, {"Limit", "int"}},
		"BatchSize": {{"Limit", "int"}},
	}
	t.Run("single accessor supports both SQL widths", func(t *testing.T) {
		for _, typ := range []string{"int32", "int64"} {
			sig := ctxSig([]string{"[]Category", "error"}, param{"size", "BatchSize"})
			got, ok := r.callArgs("Sample", sig, []string{"ctx", "size"}, querySig{params: []param{{"limit", typ}}})
			if !ok || got != "ctx, "+typ+"(size.Limit())" {
				t.Fatalf("call = %q, %v", got, ok)
			}
		}
	})
	t.Run("positional arguments match accessors by name", func(t *testing.T) {
		sig := ctxSig([]string{"[]Category", "error"}, param{"page", "BatchPage"})
		query := querySig{params: []param{{"limit", "int64"}, {"afterID", "int64"}}}
		got, ok := r.callArgs("List", sig, []string{"ctx", "page"}, query)
		if !ok || got != "ctx, int64(page.Limit()), page.AfterID()" {
			t.Fatalf("call = %q, %v", got, ok)
		}
		query.params[1].name = "beforeID"
		if _, ok := r.callArgs("List", sig, []string{"ctx", "page"}, query); ok {
			t.Fatal("unmatched accessor accepted")
		}
	})
	t.Run("duplicate accessor and scalar names are ambiguous", func(t *testing.T) {
		sig := ctxSig([]string{"[]Category", "error"}, param{"page", "BatchPage"}, param{"limit", "int"})
		query := querySig{params: []param{{"afterID", "int64"}, {"limit", "int64"}, {"otherLimit", "int64"}}}
		if _, ok := r.callArgs("List", sig, []string{"ctx", "page", "limit"}, query); ok {
			t.Fatal("duplicate parameter name accepted")
		}
	})
}

func TestValueObjectShapes(t *testing.T) {
	values, err := valueObjects("../../internal/repository")
	if err != nil {
		t.Fatal(err)
	}
	r := testRenderer()
	r.values = values
	r.gen["ListCategoriesParams"] = map[string]string{"AfterID": "int64", "Limit": "int32"}
	r.queries["ListCategories"] = querySig{params: []param{{"arg", "ListCategoriesParams"}}, results: []string{"[]Category", "error"}}
	sig := ctxSig([]string{"[]Category", "error"}, param{"page", "BatchPage"})
	hand := `func (a *PGAdapter) ListCategories(ctx context.Context, page repository.BatchPage) ([]repository.Category, error) {
	if err := page.Validate(); err != nil { return nil, err }
	rows, err := a.queries.ListCategories(ctx, pggen.ListCategoriesParams{AfterID: page.AfterID(), Limit: int32(page.Limit())})
	if err != nil { return nil, err }
	return pgCategoriesToDomain(rows), nil
}`
	if harvested(t, r, "ListCategories", sig, hand) == nil {
		t.Fatalf("validated value object was not harvested: %+v", r.candidates("ListCategories", sig))
	}
	for _, invalid := range []string{
		strings.Replace(hand, "if err := page.Validate(); err != nil { return nil, err }", "", 1),
		strings.Replace(hand, "AfterID: page.AfterID()", "AfterID: int64(page.Limit())", 1),
		strings.Replace(hand, "return nil, err }", "return nil, nil }", 1),
	} {
		if harvested(t, r, "ListCategories", sig, invalid) != nil {
			t.Fatal("changed validation or field semantics were harvested")
		}
	}
	delete(r.gen["ListCategoriesParams"], "Limit")
	r.gen["ListCategoriesParams"]["PageSize"] = "int32"
	if len(r.misnamedParams("ListCategories", sig)) != 1 || len(r.candidates("ListCategories", sig)) != 0 {
		t.Fatal("accessor/query parameter mismatch was not rejected")
	}
}

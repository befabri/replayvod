package main

import (
	"strings"
	"testing"
)

func TestInlineLiteralMatchesGeneratedRowMapper(t *testing.T) {
	r := testRenderer()
	r.domain = map[string]map[string]string{"Session": {"HashedID": "string", "UserAgent": "*string"}}
	r.gen["Session"] = map[string]string{"HashedID": "string", "UserAgent": "*string"}
	r.queries["GetSession"] = querySig{params: []param{{"hashedID", "string"}}, results: []string{"Session", "error"}}
	sig := ctxSig([]string{"*Session", "error"}, param{"hashedID", "string"})
	hand := `func (a *PGAdapter) GetSession(ctx context.Context, hashedID string) (*repository.Session, error) {
	row, err := a.queries.GetSession(ctx, hashedID)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.Session{UserAgent: row.UserAgent, HashedID: row.HashedID}, nil
}
`
	c := harvested(t, r, "GetSession", sig, hand)
	if c == nil || !strings.Contains(c.emit, "return pgSessionToDomain(row), nil") {
		t.Fatalf("inline literal was not harvested onto the generated mapper: %+v", c)
	}
	for _, changed := range []string{
		strings.Replace(hand, "HashedID: row.HashedID", "HashedID: row.UserAgent", 1),
		strings.Replace(hand, "UserAgent: row.UserAgent, ", "", 1),
	} {
		if harvested(t, r, "GetSession", sig, changed) != nil {
			t.Fatal("literal with different fields was harvested")
		}
	}
}

func TestEmptySliceGuardIsAnAcceptedSliceShape(t *testing.T) {
	r := testRenderer()
	r.queries["ListCategoriesByIDs"] = querySig{params: []param{{"ids", "[]string"}}, results: []string{"[]Category", "error"}}
	sig := ctxSig([]string{"[]Category", "error"}, param{"ids", "[]string"})
	plain := `func (a *PGAdapter) ListCategoriesByIDs(ctx context.Context, ids []string) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pg list categories by ids: %w", err)
	}
	return pgCategoriesToDomain(rows), nil
}
`
	guard := "\tif len(ids) == 0 {\n\t\treturn []repository.Category{}, nil\n\t}\n"
	guarded := strings.Replace(plain, "{\n\trows", "{\n"+guard+"\trows", 1)
	if c := harvested(t, r, "ListCategoriesByIDs", sig, plain); c == nil || strings.Contains(c.emit, "len(ids)") {
		t.Fatalf("unguarded body gained a guard: %+v", c)
	}
	if c := harvested(t, r, "ListCategoriesByIDs", sig, guarded); c == nil || !strings.Contains(c.emit, guard) {
		t.Fatalf("guarded body lost its guard: %+v", c)
	}
	for _, changed := range []string{
		strings.Replace(guarded, "return []repository.Category{}, nil", "return nil, nil", 1),
		strings.Replace(guarded, "len(ids) == 0", "len(ids) == 1", 1),
	} {
		if harvested(t, r, "ListCategoriesByIDs", sig, changed) != nil {
			t.Fatal("a different short-circuit was harvested")
		}
	}
}

func TestDiscardedRowExecShape(t *testing.T) {
	r := testRenderer()
	r.queries["SetTaskNextRun"] = querySig{params: []param{{"name", "string"}}, results: []string{"Task", "error"}}
	sig := ctxSig([]string{"error"}, param{"name", "string"})
	bare := `func (a *PGAdapter) SetTaskNextRun(ctx context.Context, name string) error {
	_, err := a.queries.SetTaskNextRun(ctx, name)
	return mapErr(err)
}
`
	ifForm := `func (a *PGAdapter) SetTaskNextRun(ctx context.Context, name string) error {
	if _, err := a.queries.SetTaskNextRun(ctx, name); err != nil {
		return mapErr(err)
	}
	return nil
}
`
	for _, hand := range []string{bare, ifForm} {
		if c := harvested(t, r, "SetTaskNextRun", sig, hand); c == nil || c.emit != bare {
			t.Fatalf("discarded row was not harvested onto the bare form: %+v", c)
		}
	}
	wrap := `return fmt.Errorf("pg set task next run: %w", err)`
	wrapped := strings.Replace(ifForm, "return mapErr(err)", wrap, 1)
	if c := harvested(t, r, "SetTaskNextRun", sig, wrapped); c == nil || c.emit != wrapped {
		t.Fatalf("wrapped discard must keep the if form: %+v", c)
	}
	if harvested(t, r, "SetTaskNextRun", sig, strings.Replace(bare, "return mapErr(err)", wrap, 1)) != nil {
		t.Fatal("a wrap of a possibly nil error was harvested")
	}
}

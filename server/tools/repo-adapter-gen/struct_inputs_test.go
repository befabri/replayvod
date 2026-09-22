package main

import (
	"strings"
	"testing"
)

func TestStructParamsExpandThroughExportedFields(t *testing.T) {
	r := testRenderer()
	r.domain = map[string]map[string]string{"ThingInput": {"ID": "string", "Name": "string", "Note": "*string", "CreatedAt": "time.Time"}}
	r.gen["UpsertThingParams"] = map[string]string{"ID": "string", "Name": "string", "Note": "*string"}
	r.queries["UpsertThing"] = querySig{params: []param{{"arg", "UpsertThingParams"}}, results: []string{"Thing", "error"}}
	r.pkgFuncs["pgThingToDomain"] = true
	sig := ctxSig([]string{"*Thing", "error"}, param{"input", "*ThingInput"})
	hand := `func (a *PGAdapter) UpsertThing(ctx context.Context, input *repository.ThingInput) (*repository.Thing, error) {
	row, err := a.queries.UpsertThing(ctx, pggen.UpsertThingParams{ID: input.ID, Name: input.Name, Note: input.Note})
	if err != nil {
		return nil, fmt.Errorf("pg upsert thing %s: %w", input.ID, err)
	}
	return pgThingToDomain(row), nil
}
`
	c := harvested(t, r, "UpsertThing", sig, hand)
	if c == nil || !strings.HasPrefix(c.emit, "func (a *PGAdapter) UpsertThing(ctx context.Context, input *repository.ThingInput)") {
		t.Fatalf("struct input with an unused field and a field verb was not harvested: %+v", c)
	}
	for _, changed := range []string{
		strings.Replace(hand, "Name: input.Name", "Name: input.ID", 1),
		strings.Replace(hand, "Note: input.Note", "Note: nil", 1),
		strings.Replace(hand, ", Note: input.Note", "", 1),
	} {
		if harvested(t, r, "UpsertThing", sig, changed) != nil {
			t.Fatal("different field semantics were harvested")
		}
	}
	if m := r.misnamedParams("UpsertThing", sig); len(m) != 0 {
		t.Fatalf("unused struct field reported as misnamed: %v", m)
	}
	r.gen["UpsertThingParams"]["Role"] = "string"
	if len(r.candidates("UpsertThing", sig)) != 0 {
		t.Fatal("a query parameter the struct lacks was guessed")
	}
}

func TestStructParamsConvertAndKeepNamesUnambiguous(t *testing.T) {
	r := testRenderer()
	r.d = dialect{name: "sqlite", genPkg: "sqlitegen", adapterType: "SQLiteAdapter"}
	r.domain = map[string]map[string]string{"SessionInput": {"HashedID": "string", "ExpiresAt": "time.Time", "Note": "*string"}}
	r.gen["CreateSessionParams"] = map[string]string{"HashedID": "string", "ExpiresAt": "sqlitetype.Time", "Note": "sql.NullString"}
	r.queries["CreateSession"] = querySig{params: []param{{"arg", "CreateSessionParams"}}, results: []string{"error"}}
	hand := `func (a *SQLiteAdapter) CreateSession(ctx context.Context, s *repository.SessionInput) error {
	return a.queries.CreateSession(ctx, sqlitegen.CreateSessionParams{HashedID: s.HashedID, ExpiresAt: sqliteTime(s.ExpiresAt), Note: toNullString(s.Note)})
}
`
	if harvested(t, r, "CreateSession", ctxSig([]string{"error"}, param{"s", "*SessionInput"}), hand) == nil {
		t.Fatal("struct fields were not converted through the argument rules")
	}
	for name, sig := range map[string]methodSig{
		"unused scalar beside the struct": ctxSig([]string{"error"}, param{"s", "*SessionInput"}, param{"userID", "string"}),
		"field colliding with a scalar":   ctxSig([]string{"error"}, param{"s", "*SessionInput"}, param{"hashedID", "string"}),
		"slice of structs":                ctxSig([]string{"error"}, param{"s", "[]SessionInput"}),
	} {
		if len(r.candidates("CreateSession", sig)) != 0 {
			t.Fatalf("%s produced a candidate", name)
		}
	}
	r.queries["DeleteSession"] = querySig{params: []param{{"hashedID", "string"}}, results: []string{"error"}}
	got, ok := r.callArgs("DeleteSession", ctxSig([]string{"error"}, param{"s", "*SessionInput"}), []string{"ctx", "s"}, r.queries["DeleteSession"])
	if !ok || got != "ctx, s.HashedID" {
		t.Fatalf("positional parameter did not pick the struct field by name: %q, %v", got, ok)
	}
}

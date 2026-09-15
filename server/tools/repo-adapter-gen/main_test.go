package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustNormalize(t *testing.T, src string) string {
	t.Helper()
	n, err := normalizeFuncSrc(src)
	if err != nil {
		t.Fatalf("normalize %q: %v", src, err)
	}
	return n
}

func TestNormalizeIgnoresSpelling(t *testing.T) {
	canonical := `func (a *PGAdapter) ListUsers(ctx context.Context, limit, offset int) ([]repository.User, error) {
	rows, err := a.queries.ListUsers(ctx, pggen.ListUsersParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, fmt.Errorf("pg list users: %w", err)
	}
	out := make([]repository.User, len(rows))
	for i, r := range rows {
		out[i] = *pgUserToDomain(r)
	}
	return out, nil
}
`
	respelled := `// ListUsers returns every user.
func (db *PGAdapter) ListUsers(c context.Context, limit int, offset int) ([]repository.User, error) {
	// multi-line literal, reversed keys, renamed locals
	rs, e := db.queries.ListUsers(c, pggen.ListUsersParams{
		Offset: int32(offset),
		Limit:  int32(limit),
	})
	if e != nil {
		return nil, fmt.Errorf("pg list users: %w", e)
	}

	users := make([]repository.User, len(rs))
	for idx, row := range rs {
		users[idx] = *pgUserToDomain(row)
	}
	return users, nil
}
`
	if mustNormalize(t, canonical) != mustNormalize(t, respelled) {
		t.Fatalf("spelling differences were not normalized:\n%s\n---\n%s", mustNormalize(t, canonical), mustNormalize(t, respelled))
	}
}

func TestNormalizeKeepsMeaning(t *testing.T) {
	base := `func (a *PGAdapter) Set(ctx context.Context, id, role string) error {
	return a.queries.Set(ctx, pggen.SetParams{ID: id, Role: role})
}
`
	for name, variant := range map[string]string{
		"swapped arguments": `func (a *PGAdapter) Set(ctx context.Context, id, role string) error {
	return a.queries.Set(ctx, pggen.SetParams{ID: role, Role: id})
}
`,
		"swapped parameters": `func (a *PGAdapter) Set(ctx context.Context, role, id string) error {
	return a.queries.Set(ctx, pggen.SetParams{ID: id, Role: role})
}
`,
		"different message": `func (a *PGAdapter) Set(ctx context.Context, id, role string) error {
	return mapErr(a.queries.Set(ctx, pggen.SetParams{ID: id, Role: role}))
}
`,
		"extra statement": `func (a *PGAdapter) Set(ctx context.Context, id, role string) error {
	if id == "" {
		return nil
	}
	return a.queries.Set(ctx, pggen.SetParams{ID: id, Role: role})
}
`,
		"different field": `func (a *PGAdapter) Set(ctx context.Context, id, role string) error {
	return a.queries.Set(ctx, pggen.SetParams{ID: id, Name: role})
}
`,
	} {
		if mustNormalize(t, base) == mustNormalize(t, variant) {
			t.Errorf("%s normalized equal to the base", name)
		}
	}
}

func TestNormalizeRewritesNullHelpers(t *testing.T) {
	helper := `func (a *SQLiteAdapter) Note(ctx context.Context, id int64, note string) error {
	return a.queries.Note(ctx, sqlitegen.NoteParams{ID: id, Note: toNullString(&note)})
}
`
	literal := `func (a *SQLiteAdapter) Note(ctx context.Context, id int64, note string) error {
	return a.queries.Note(ctx, sqlitegen.NoteParams{ID: id, Note: sql.NullString{String: note, Valid: true}})
}
`
	pointer := `func (a *SQLiteAdapter) Note(ctx context.Context, id int64, note string) error {
	return a.queries.Note(ctx, sqlitegen.NoteParams{ID: id, Note: toNullString(note)})
}
`
	if mustNormalize(t, helper) != mustNormalize(t, literal) {
		t.Errorf("toNullString(&x) was not rewritten to the literal")
	}
	if mustNormalize(t, pointer) == mustNormalize(t, literal) {
		t.Errorf("toNullString(x) on a pointer must not equal the always-valid literal")
	}
}

func testRenderer() renderer {
	return renderer{
		d: dialect{name: "pg", dir: "internal/repository/pgadapter", genPkg: "pggen", adapterType: "PGAdapter", rowLocks: map[string]bool{"LockThing": true}},
		gen: map[string]map[string]string{
			"UpdateThingParams": {"ID": "string", "Note": "*string"},
		},
		queries: map[string]querySig{
			"ListCategories": {results: []string{"[]Category", "error"}},
			"ListInvites":    {results: []string{"[]Invite", "error"}},
			"UpdateThing":    {params: []param{{"arg", "UpdateThingParams"}}, results: []string{"error"}},
			"DeleteThing":    {params: []param{{"id", "string"}}, results: []string{"int64", "error"}},
			"CountThings":    {results: []string{"int64", "error"}},
			"LockThing":      {params: []param{{"id", "string"}}, results: []string{"Category", "error"}},
			"ListSnapshots":  {results: []string{"[]EventsubSnapshot", "error"}},
		},
		pkgFuncs: map[string]bool{"pgInviteToDomain": true},
	}
}

func ctxSig(results []string, params ...param) methodSig {
	return methodSig{params: append([]param{{"ctx", "context.Context"}}, params...), results: results}
}

// harvested reports the candidate a hand-written body would be replaced by.
func harvested(t *testing.T, r renderer, name string, sig methodSig, hand string) *candidate {
	t.Helper()
	c, err := matchCandidate(mustNormalize(t, hand), r.candidates(name, sig), true)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInlineLoopMatchesGeneratedSliceMapper(t *testing.T) {
	r := testRenderer()
	hand := `func (a *PGAdapter) ListCategories(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list categories: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}
`
	c := harvested(t, r, "ListCategories", ctxSig([]string{"[]Category", "error"}), hand)
	if c == nil {
		t.Fatal("inline loop over a generated mapper was not harvested")
	}
	if !strings.Contains(c.emit, "return pgCategoriesToDomain(rows), nil") {
		t.Errorf("emitted form should call the plural mapper:\n%s", c.emit)
	}
}

func TestInlineLoopStaysInlineForHandWrittenMapper(t *testing.T) {
	r := testRenderer()
	hand := `func (a *PGAdapter) ListInvites(ctx context.Context) ([]repository.Invite, error) {
	rows, err := a.queries.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]repository.Invite, len(rows))
	for i, r := range rows {
		out[i] = *pgInviteToDomain(r)
	}
	return out, nil
}
`
	c := harvested(t, r, "ListInvites", ctxSig([]string{"[]Invite", "error"}), hand)
	if c == nil {
		t.Fatal("inline loop over a hand-written mapper was not harvested")
	}
	if !strings.Contains(c.emit, "*pgInviteToDomain(r)") || strings.Contains(c.emit, "pgInvitesToDomain") {
		t.Errorf("emitted form should keep the loop when no plural mapper exists:\n%s", c.emit)
	}
}

func TestErrorShapes(t *testing.T) {
	r := testRenderer()
	sig := ctxSig([]string{"error"}, param{"id", "string"}, param{"note", "string"})
	for name, hand := range map[string]string{
		"wrapped with argument": `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	if err := a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: id, Note: &note}); err != nil {
		return fmt.Errorf("pg update thing %s: %w", id, err)
	}
	return nil
}
`,
		"mapErr": `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	return mapErr(a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: id, Note: &note}))
}
`,
	} {
		if harvested(t, r, "UpdateThing", sig, hand) == nil {
			t.Errorf("%s was not harvested", name)
		}
	}
	unknownMessage := `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	if err := a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: id, Note: &note}); err != nil {
		return fmt.Errorf("pg update the thing: %w", err)
	}
	return nil
}
`
	if harvested(t, r, "UpdateThing", sig, unknownMessage) != nil {
		t.Error("a message the generator cannot derive was harvested")
	}
	swapped := `func (a *PGAdapter) UpdateThing(ctx context.Context, id, note string) error {
	return a.queries.UpdateThing(ctx, pggen.UpdateThingParams{ID: note, Note: &id})
}
`
	if r.candidates("UpdateThing", sig) == nil {
		t.Fatal("UpdateThing fits no shape")
	}
	if harvested(t, r, "UpdateThing", sig, swapped) != nil {
		t.Error("swapped same-typed arguments were harvested")
	}
}

func TestRowsAffectedAndScalarShapes(t *testing.T) {
	r := testRenderer()
	notFound := `func (a *PGAdapter) DeleteThing(ctx context.Context, id string) error {
	n, err := a.queries.DeleteThing(ctx, id)
	if err != nil {
		return fmt.Errorf("pg delete thing: %w", mapErr(err))
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}
`
	if harvested(t, r, "DeleteThing", ctxSig([]string{"error"}, param{"id", "string"}), notFound) == nil {
		t.Error("rows-affected to ErrNotFound was not harvested")
	}
	deleted := `func (a *PGAdapter) DeleteThing(ctx context.Context, id string) (bool, error) {
	affected, err := a.queries.DeleteThing(ctx, id)
	if err != nil {
		return false, fmt.Errorf("pg delete thing %q: %w", id, err)
	}
	return affected > 0, nil
}
`
	if harvested(t, r, "DeleteThing", ctxSig([]string{"bool", "error"}, param{"id", "string"}), deleted) == nil {
		t.Error("rows-affected to bool was not harvested")
	}
	count := `func (a *PGAdapter) CountThings(ctx context.Context) (int64, error) {
	return a.queries.CountThings(ctx)
}
`
	if harvested(t, r, "CountThings", ctxSig([]string{"int64", "error"}), count) == nil {
		t.Error("direct scalar return was not harvested")
	}
	if r.candidates("CountThings", ctxSig([]string{"string", "error"})) != nil {
		t.Error("a scalar with no result conversion fits no shape")
	}
}

func TestRowLockGuardAndAlias(t *testing.T) {
	r := testRenderer()
	locked := `func (a *PGAdapter) LockThing(ctx context.Context, id string) (*repository.Category, error) {
	if !a.inTransaction() {
		return nil, repository.ErrNoTransaction
	}
	row, err := a.queries.LockThing(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgCategoryToDomain(row), nil
}
`
	sig := ctxSig([]string{"*Category", "error"}, param{"id", "string"})
	if harvested(t, r, "LockThing", sig, locked) == nil {
		t.Error("guarded row lock was not harvested")
	}
	unguarded := strings.Replace(locked, "\tif !a.inTransaction() {\n\t\treturn nil, repository.ErrNoTransaction\n\t}\n", "", 1)
	if harvested(t, r, "LockThing", sig, unguarded) != nil {
		t.Error("row lock without the transaction guard was harvested")
	}
	aliased := `func (a *PGAdapter) ListEventSubSnapshots(ctx context.Context) ([]repository.EventSubSnapshot, error) {
	rows, err := a.queries.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list snapshots: %w", err)
	}
	out := make([]repository.EventSubSnapshot, len(rows))
	for i, r := range rows {
		out[i] = *pgSnapshotToDomain(r)
	}
	return out, nil
}
`
	c := harvested(t, r, "ListEventSubSnapshots", ctxSig([]string{"[]EventSubSnapshot", "error"}), aliased)
	if c == nil || !strings.Contains(c.emit, "pgSnapshotsToDomain(rows)") {
		t.Errorf("aliased query was not harvested onto the generated mapper: %+v", c)
	}
}

func TestRatchet(t *testing.T) {
	path := filepath.Join(t.TempDir(), baselineFile)
	if err := ratchet(path, map[string]int{"pgadapter": 10, "sqliteadapter": 12}, true); err == nil {
		t.Error("check mode accepted a missing baseline")
	}
	if err := ratchet(path, map[string]int{"pgadapter": 10, "sqliteadapter": 12}, false); err != nil {
		t.Fatal(err)
	}
	if err := ratchet(path, map[string]int{"pgadapter": 10, "sqliteadapter": 12}, true); err != nil {
		t.Errorf("check mode rejected an exact baseline: %v", err)
	}
	if err := ratchet(path, map[string]int{"pgadapter": 11, "sqliteadapter": 12}, true); err == nil {
		t.Error("check mode accepted a rising count")
	}
	if err := ratchet(path, map[string]int{"pgadapter": 11, "sqliteadapter": 12}, false); err == nil {
		t.Error("generating mode accepted a rising count")
	}
	if err := ratchet(path, map[string]int{"pgadapter": 9, "sqliteadapter": 12}, true); err == nil {
		t.Error("check mode accepted a stale baseline")
	}
	if err := ratchet(path, map[string]int{"pgadapter": 9, "sqliteadapter": 12}, false); err != nil {
		t.Fatal(err)
	}
	got, err := readBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["pgadapter"] != 9 || got["sqliteadapter"] != 12 {
		t.Errorf("baseline after lowering = %v", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), "# ") {
		t.Errorf("baseline lost its header:\n%s", raw)
	}
}

func TestActionPhrases(t *testing.T) {
	for name, want := range map[string][]string{
		"UpsertTitle":                 {"upsert title"},
		"EnsureServerHMACSecret":      {"ensure server hmac secret", "ensure server HMAC secret"},
		"SetStorageID":                {"set storage id", "set storage ID"},
		"ListCategoriesByIDs":         {"list categories by ids", "list categories by IDs"},
		"GetOpenVideoByTwitchVideoID": {"get open video by twitch video id", "get open video by twitch video ID"},
		"ListURLsForVideo":            {"list urls for video", "list URLs for video"},
		"A":                           {"a"},
	} {
		got := actionPhrases(name)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("actionPhrases(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestScalarShapes(t *testing.T) {
	r := testRenderer()
	r.queries["HasParts"] = querySig{params: []param{{"videoID", "int64"}}, results: []string{"int64", "error"}}
	r.queries["GetKey"] = querySig{params: []param{{"videoID", "int64"}}, results: []string{"string", "error"}}
	flattened := `func (a *PGAdapter) HasParts(ctx context.Context, videoID int64) (bool, error) {
	// EXISTS comes back as 0/1.
	v, err := a.queries.HasParts(ctx, videoID)
	if err != nil {
		return false, err
	}
	return v != 0, nil
}
`
	c := harvested(t, r, "HasParts", ctxSig([]string{"bool", "error"}, param{"videoID", "int64"}), flattened)
	if c == nil || !strings.Contains(c.emit, "return v != 0, nil") {
		t.Errorf("int64 to bool result conversion was not harvested: %+v", c)
	}
	compact := `func (a *PGAdapter) GetKey(ctx context.Context, videoID int64) (string, error) {
	key, err := a.queries.GetKey(ctx, videoID)
	return key, mapErr(err)
}
`
	c = harvested(t, r, "GetKey", ctxSig([]string{"string", "error"}, param{"videoID", "int64"}), compact)
	if c == nil || !strings.Contains(c.emit, `return "", mapErr(err)`) {
		t.Errorf("compact mapErr scalar was not harvested onto the if form: %+v", c)
	}
	tuple := strings.Replace(compact, "return key, mapErr(err)", "return key, err", 1)
	c = harvested(t, r, "GetKey", ctxSig([]string{"string", "error"}, param{"videoID", "int64"}), tuple)
	if c == nil || !strings.HasSuffix(c.emit, "\treturn a.queries.GetKey(ctx, videoID)\n}\n") {
		t.Errorf("bare scalar pass-through should emit the direct return: %+v", c)
	}
}

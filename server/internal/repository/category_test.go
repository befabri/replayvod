package repository

import "testing"

func TestUniqueCategoriesByID(t *testing.T) {
	in := []Category{
		{ID: "b", Name: "First B"},
		{ID: "a", Name: "A"},
		{ID: "b", Name: "Second B"},
	}
	got := UniqueCategoriesByID(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "First B" || got[1].Name != "A" {
		t.Fatalf("order/dedupe = %+v, want first b then a", got)
	}
}

func TestOrderCategoriesByIDs(t *testing.T) {
	rows := []Category{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	}
	got := OrderCategoriesByIDs(rows, []string{"c", "missing", "a", "c", "b"})
	want := []string{"c", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q; rows=%+v", i, got[i].ID, id, got)
		}
	}
}

package contracttest

import (
	"context"
	"testing"
)

func testPing(t *testing.T, h Harness) {
	repo := h.Repo()
	if err := repo.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repo.Ping(ctx); err == nil {
		t.Fatal("a cancelled context reported the database as reachable")
	}
}

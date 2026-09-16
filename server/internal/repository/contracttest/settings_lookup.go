package contracttest

import (
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testSettingsLookup(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	if _, err := repo.GetSettings(ctx, "owner"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("settings before any row: %v", err)
	}
	ensured, err := repo.EnsureSettings(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSettings(ctx, "owner")
	if err != nil || got.UserID != "owner" || got.Timezone != ensured.Timezone || got.Language != ensured.Language || got.DatetimeFormat != ensured.DatetimeFormat {
		t.Fatalf("settings = %+v, %v; ensured %+v", got, err, ensured)
	}
	if _, err := repo.GetSettings(ctx, "nobody"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("settings of an unknown user: %v", err)
	}
}

func testTaskListing(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	if _, err := repo.UpsertTask(ctx, "zeta_cleanup", "Z", 60); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTask(ctx, "alpha_scan", "A", 0); err != nil {
		t.Fatal(err)
	}
	tasks, err := repo.ListTasks(ctx)
	if err != nil || len(tasks) != 2 || tasks[0].Name != "alpha_scan" || tasks[1].Name != "zeta_cleanup" || tasks[1].IntervalSeconds != 60 {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
}

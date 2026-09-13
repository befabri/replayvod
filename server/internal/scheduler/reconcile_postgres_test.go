//go:build integration

package scheduler

import (
	"os"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestMain(m *testing.M) {
	os.Exit(testdb.SetupPG(m))
}

func TestReconcileTasksPostgres(t *testing.T) {
	testReconcileTasks(t, func(t *testing.T) repository.Repository {
		return pgadapter.New(testdb.NewPGPool(t))
	})
}

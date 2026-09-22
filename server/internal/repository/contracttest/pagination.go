package contracttest

import (
	"github.com/befabri/replayvod/server/internal/repository"
	"testing"
)

func batchPage(t *testing.T, afterID int64, limit int) repository.BatchPage {
	t.Helper()
	page, err := repository.NewBatchPage(afterID, limit)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func batchSize(t *testing.T, limit int) repository.BatchSize {
	t.Helper()
	size, err := repository.NewBatchSize(limit)
	if err != nil {
		t.Fatal(err)
	}
	return size
}

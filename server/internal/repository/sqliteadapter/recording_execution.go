package sqliteadapter

import (
	"github.com/befabri/replayvod/server/internal/repository"
)

func executionAffected(n int64, err error) error {
	if err != nil {
		return err
	}
	if n != 1 {
		return repository.ErrStaleExecution
	}
	return nil
}

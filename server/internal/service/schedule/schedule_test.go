package schedule

import (
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestValidateFilterConsistencyRejectsRetentionWindowOverflow(t *testing.T) {
	tooLarge := repository.MaxRetentionWindowHours + 1
	err := validateFilterConsistency(false, nil, true, &tooLarge)
	if !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("validateFilterConsistency err = %v, want ErrInvalidFilter", err)
	}
}

// TestValidateFilterConsistencyAllowsStaleWindowWhenDeleteOff pins that the
// bound is gated on is_delete_rediff, matching the DB CHECK: when delete is off,
// time_before_delete is a dead field, so a stale over-ceiling value is allowed
// rather than rejected. The API handler no longer carries an unconditional tag
// that would contradict this.
func TestValidateFilterConsistencyAllowsStaleWindowWhenDeleteOff(t *testing.T) {
	tooLarge := repository.MaxRetentionWindowHours + 1
	if err := validateFilterConsistency(false, nil, false, &tooLarge); err != nil {
		t.Fatalf("validateFilterConsistency(deleteOff, staleWindow) = %v, want nil", err)
	}
}

package playbackcache

import (
	"context"
	"fmt"
	"syscall"
)

func (s *Service) currentCacheBytes(ctx context.Context) int64 {
	total, err := s.repo.SumReadyPlaybackBytes(ctx)
	if err != nil {
		s.log.Warn("sum playback cache bytes failed", "error", err)
	}
	return total
}

// cacheBudget separates the configured size limit from temporary disk pressure.
type cacheBudget struct {
	// configured is the limit derived from total local disk or recorded-library
	// bytes on object storage, independent of temporary free space.
	configured int64
	// current includes reclaimable cache bytes when calculating the eviction target.
	current int64
	// buildHeadroom excludes existing cache bytes because pruning follows the
	// build; counting them would admit output that exceeds actual free space.
	buildHeadroom int64
	known         bool
}

// capacity caps local storage against filesystem size and free space.
// Object storage uses the recorded library size as its reference.
func (s *Service) capacity(ctx context.Context, maxPercent int, currentCacheBytes int64) (cacheBudget, error) {
	if s.capacityOverride != nil {
		b, known := s.capacityOverride(currentCacheBytes)
		return cacheBudget{configured: b, current: b, buildHeadroom: b, known: known}, nil
	}
	if root := s.store.CapacityRoot(); root != "" {
		total, avail, err := s.fsStat(root)
		if err != nil {
			return cacheBudget{}, fmt.Errorf("stat storage filesystem: %w", err)
		}
		// Divide before multiply: total is a whole-filesystem byte count and
		// total*maxPercent could overflow int64 on a very large array.
		configured := max(total/100*int64(maxPercent), 0)
		reserve := total / diskReserveFraction
		return cacheBudget{
			configured:    configured,
			current:       max(min(configured, currentCacheBytes+avail-reserve), 0),
			buildHeadroom: max(min(configured, avail-reserve), 0),
			known:         true,
		}, nil
	}
	totals, err := s.repo.VideoStatsTotals(ctx, "")
	if err != nil {
		return cacheBudget{}, fmt.Errorf("read library totals for playback cache cap: %w", err)
	}
	configured := max(totals.TotalSize/100*int64(maxPercent), 0)
	return cacheBudget{configured: configured, current: configured, buildHeadroom: configured, known: true}, nil
}

func statfsBytes(root string) (int64, int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, 0, err
	}
	return int64(stat.Blocks) * int64(stat.Bsize), int64(stat.Bavail) * int64(stat.Bsize), nil
}

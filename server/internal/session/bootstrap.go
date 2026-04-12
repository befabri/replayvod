package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
)

// SeedWhitelist adds the OWNER_TWITCH_ID and any WHITELISTED_USER_IDS from
// config to the whitelist table. Idempotent — safe to call on every startup.
//
// Runtime auth only consults the DB, so config is a bootstrap mechanism:
//   - Guarantees the owner can always log in even with an empty whitelist
//   - Provides a way to pre-populate allowed users before the admin UI exists
func SeedWhitelist(ctx context.Context, repo repository.Repository, ownerTwitchID, whitelistedIDs string, log *slog.Logger) error {
	ids := make([]string, 0)
	if ownerTwitchID != "" {
		ids = append(ids, ownerTwitchID)
	}
	for _, id := range strings.Split(whitelistedIDs, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	seeded := 0
	for _, id := range ids {
		exists, err := repo.IsWhitelisted(ctx, id)
		if err != nil {
			return fmt.Errorf("check whitelist %s: %w", id, err)
		}
		if exists {
			continue
		}
		if err := repo.AddToWhitelist(ctx, id); err != nil {
			return fmt.Errorf("seed whitelist %s: %w", id, err)
		}
		seeded++
	}

	if seeded > 0 {
		log.Info("Seeded whitelist from config", "count", seeded)
	}
	return nil
}

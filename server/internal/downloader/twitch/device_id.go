package twitch

import (
	"crypto/rand"
	"encoding/hex"
)

// generateDeviceID returns a stable-within-process pseudo-random
// hex string Twitch accepts as a Device-Id. The real web player
// uses a persisted UUID; we use 16 random bytes because we don't
// need persistence across restarts (Twitch only correlates Device-
// Id to integrity tokens, both of which we re-acquire on a fresh
// process start anyway).
func generateDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively impossible on
		// Linux; returning a constant keeps the client usable
		// at the cost of less isolation — Twitch tolerates
		// duplicate device IDs.
		return "0123456789abcdef0123456789abcdef"
	}
	return hex.EncodeToString(b)
}

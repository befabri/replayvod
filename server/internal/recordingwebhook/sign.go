package recordingwebhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Outbound delivery headers. The names are ReplayVOD-specific, but the values
// follow the inbound Twitch EventSub convention so a receiver verifies a
// delivery with the same HMAC-SHA256(id‖timestamp‖body) computation.
const (
	HeaderID        = "Replayvod-Webhook-Id"
	HeaderTimestamp = "Replayvod-Webhook-Timestamp"
	HeaderSignature = "Replayvod-Webhook-Signature"
	HeaderEvent     = "Replayvod-Webhook-Event"
)

// sign returns the value for HeaderSignature: "sha256=" + hex(HMAC-SHA256(
// secret, id || timestamp || body)). This is byte-for-byte the scheme
// twitch.VerifyEventSubSignature checks against, so a receiver that already
// verifies Twitch EventSub deliveries verifies these with the same code, just
// reading the ReplayVOD header names. The hex is lowercase, matching the
// inbound verifier's ToLower comparison.
func sign(secret, id, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newMessageID returns a random 16-byte hex id, unique per delivery. The
// receiver can use it for idempotency the same way it would use
// Twitch-Eventsub-Message-Id. Used for test sends, which are intentionally
// distinct per invocation; terminal events use terminalMessageID instead.
func newMessageID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate webhook message id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// terminalMessageID derives the receiver-facing idempotency id for a terminal
// recording event deterministically from its (event, videoID) tuple. Deriving
// it rather than drawing random bytes has two payoffs: it can never fail (no
// RNG read), so a terminal video transition always has a row to enqueue and can
// never finalize the video while silently dropping its webhook; and it is
// stable across retries of the same event, which is exactly the idempotency
// contract a receiver wants from a message id. It encodes the same (event,
// videoID) identity the dedupe key already carries, rendered in the same
// 32-hex-char shape as newMessageID so receivers see a uniform
// Replayvod-Webhook-Id regardless of how the row was minted.
func terminalMessageID(event string, videoID int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "recording-webhook/terminal/v1\n%s\n%d", event, videoID))
	return hex.EncodeToString(sum[:16])
}

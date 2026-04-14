// Twitch-specific URL helpers shared across the dashboard.

// resolveBoxArtUrl normalizes Twitch's box-art template URL to a
// concrete size. Helix returns URLs like
//   https://static-cdn.jtvnw.net/ttv-boxart/{game-id}-{width}x{height}.jpg
// and the frontend picks the target dimensions. Pre-sized URLs (or any
// URL without placeholders) pass through unchanged. Returns null when
// the input is empty/missing so callers can show a placeholder.
export function resolveBoxArtUrl(
	url: string | null | undefined,
	width: number,
	height: number,
): string | null {
	if (!url) return null;
	return url
		.replace("{width}", String(width))
		.replace("{height}", String(height));
}

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

export function resolveBoxArtSrcSet(
	url: string | null | undefined,
	width: number,
	height: number,
): string | undefined {
	if (
		!url ||
		width <= 0 ||
		height <= 0 ||
		(!url.includes("{width}") && !url.includes("{height}"))
	) {
		return undefined;
	}

	const variants = [1, 1.5, 2, 3].reduce<Map<number, string>>(
		(byWidth, scale) => {
			const variantWidth = Math.round(width * scale);
			const variantHeight = Math.round(height * scale);
			const resolved = resolveBoxArtUrl(url, variantWidth, variantHeight);
			if (resolved) byWidth.set(variantWidth, resolved);
			return byWidth;
		},
		new Map(),
	);

	if (variants.size === 0) return undefined;
	return Array.from(variants, ([variantWidth, resolved]) => {
		return `${resolved} ${variantWidth}w`;
	}).join(", ");
}

export function twitchLivePreviewURL(
	login: string | null | undefined,
	options?: { width?: number; height?: number; cacheBust?: number },
): string | null {
	const normalized = login?.trim().toLowerCase();
	if (!normalized) return null;
	const width = options?.width ?? 1280;
	const height = options?.height ?? 720;
	const base = `https://static-cdn.jtvnw.net/previews-ttv/live_user_${encodeURIComponent(normalized)}-${width}x${height}.jpg`;
	return options?.cacheBust == null ? base : `${base}?rv=${options.cacheBust}`;
}

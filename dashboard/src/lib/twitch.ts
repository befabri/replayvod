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

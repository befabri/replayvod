import type { LiveRendition } from "@/api/generated/trpc";

// RenditionOption is one row of the live quality picker: a height plus the
// codec and frame rate the recorder would take at that height.
export interface RenditionOption {
	height: number;
	fps?: number;
	codec: string;
}

// renditionOptions collapses the server's per-variant list to one row per
// height, tallest first. The server puts each height's preferred variant
// ahead of its alternatives, so the first sighting of a height is the one a
// cap at that height records. Force H.264 drops the other codecs first, the
// same filter the recorder applies, so an HEVC-only height disappears rather
// than promising a file the recorder cannot make.
export function renditionOptions(
	renditions: readonly LiveRendition[],
	forceH264: boolean,
): RenditionOption[] {
	const seen = new Set<number>();
	const out: RenditionOption[] = [];
	for (const r of renditions) {
		if (forceH264 && r.codec !== "h264") continue;
		if (r.height <= 0 || seen.has(r.height)) continue;
		seen.add(r.height);
		out.push({ height: r.height, fps: r.fps, codec: r.codec });
	}
	return out.sort((a, b) => b.height - a.height);
}

// Options are tallest first. Never increase a user's ceiling when the
// playlist changes; a null selection asks them to choose another quality.
export function renditionAtOrBelow(
	options: readonly RenditionOption[],
	ceiling: number,
) {
	return options.find((option) => option.height <= ceiling)?.height ?? null;
}

// renditionLabel reads like Twitch's own quality menu: "1080p60", "720p",
// with the codec called out when it is not plain H.264. A 30 fps rendition
// carries no frame rate, as on Twitch.
export function renditionLabel(o: RenditionOption): string {
	const fps = o.fps && o.fps > 0 ? Math.round(o.fps) : 0;
	const rate = fps > 0 && fps !== 30 ? String(fps) : "";
	const codec =
		o.codec === "h265" ? " · HEVC" : o.codec === "av1" ? " · AV1" : "";
	return `${o.height}p${rate}${codec}`;
}

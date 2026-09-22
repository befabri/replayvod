import type { LiveRendition } from "@/api/generated/trpc";

export interface RenditionOption {
	height: number;
	fps?: number;
	codec: string;
}

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

export function renditionAtOrBelow(
	options: readonly RenditionOption[],
	ceiling: number,
) {
	return options.find((option) => option.height <= ceiling)?.height ?? null;
}

export function renditionLabel(o: RenditionOption): string {
	const fps = o.fps && o.fps > 0 ? Math.round(o.fps) : 0;
	const rate = fps > 0 && fps !== 30 ? String(fps) : "";
	const codec =
		o.codec === "h265" ? " · HEVC" : o.codec === "av1" ? " · AV1" : "";
	return `${o.height}p${rate}${codec}`;
}

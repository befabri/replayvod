import type { VideoResponse } from "@/api/generated/trpc";
import { positiveOrZero } from "@/lib/utils";

type VideoPart = NonNullable<VideoResponse["parts"]>[number];

export function orderedVideoParts(
	video: Pick<VideoResponse, "parts">,
): VideoPart[] {
	return [...(video.parts ?? [])].sort((a, b) => a.part_index - b.part_index);
}

export function videoPartCount(video: Pick<VideoResponse, "parts">): number {
	return orderedVideoParts(video).length;
}

export function isMultipartVideo(video: Pick<VideoResponse, "parts">): boolean {
	return videoPartCount(video) > 1;
}

export function recordingDurationSeconds(
	video: Pick<VideoResponse, "duration_seconds" | "parts">,
): number | undefined {
	const stored = positiveOrZero(video.duration_seconds);
	if (stored > 0) return stored;
	const total = orderedVideoParts(video).reduce(
		(sum, part) => sum + positiveOrZero(part.duration_seconds),
		0,
	);
	return total > 0 ? total : undefined;
}

export function recordingSizeBytes(
	video: Pick<VideoResponse, "size_bytes" | "parts">,
): number | undefined {
	const stored = positiveOrZero(video.size_bytes);
	if (stored > 0) return stored;
	const total = orderedVideoParts(video).reduce(
		(sum, part) => sum + positiveOrZero(part.size_bytes),
		0,
	);
	return total > 0 ? total : undefined;
}

export function recordingQualitySummary(
	video: Pick<VideoResponse, "quality" | "parts">,
	mixedLabel: string,
): string {
	const qualities = uniqueNonEmpty(
		orderedVideoParts(video).map((part) => part.quality),
	);
	if (qualities.length === 0) return video.quality;
	if (qualities.length === 1) return qualities[0];
	return `${mixedLabel} (${qualities.join(", ")})`;
}

function uniqueNonEmpty(values: string[]): string[] {
	const seen = new Set<string>();
	const out: string[] = [];
	for (const raw of values) {
		const value = raw.trim();
		if (!value || seen.has(value)) continue;
		seen.add(value);
		out.push(value);
	}
	return out;
}

import type { z } from "zod";
import { CreateInputSchema } from "@/api/generated/zod";

export type RecordingMode = "video" | "audio";

// RecordingQualitySchema is the quality the API accepts, read off the generated
// schedule-create input rather than restated. Every quality control in the
// dashboard takes its type, its values and its validation from here, so a
// quality added on the server reaches all of them without a hand edit. The
// server repeats the same list on every request that carries a quality;
// recording-settings.test.ts holds the anchor and the rest to each other.
export const RecordingQualitySchema = CreateInputSchema.shape.quality;
export type RecordingQuality = z.infer<typeof RecordingQualitySchema>;

// Best first, the order the pickers read in. Zod's own `options` follows the
// enum object's keys, which floats the numeric "1440" to the front, so the
// display order is spelled out. The record is total: a quality added on the
// server fails to compile until it is placed here.
const QUALITY_ORDER: Record<RecordingQuality, number> = {
	BEST: 0,
	"1440": 1,
	HIGH: 2,
	MEDIUM: 3,
	LOW: 4,
};

export const RECORDING_QUALITIES: readonly RecordingQuality[] = [
	...RecordingQualitySchema.options,
].sort((a, b) => QUALITY_ORDER[a] - QUALITY_ORDER[b]);

// qualityAboveHD reports whether a quality reaches past the 1080p tier, the
// most Twitch serves an anonymous viewer. Anything ranked above HIGH in the
// display order qualifies, so a tier added on the server above it (a 2160)
// is covered without a hand edit. Whether the extra renditions exist depends
// on the playback session the owner connected; the quality hint uses this to
// decide when to speak up.
export function qualityAboveHD(quality: RecordingQuality): boolean {
	return QUALITY_ORDER[quality] < QUALITY_ORDER.HIGH;
}

// The ceiling represented by each ladder choice. BEST has no height limit.
export const RECORDING_QUALITY_HEIGHT: Record<RecordingQuality, number> = {
	LOW: 480,
	MEDIUM: 720,
	HIGH: 1080,
	"1440": 1440,
	BEST: Number.POSITIVE_INFINITY,
};

// qualityTierForHeight is the ladder rung a pinned height falls under. The
// server stores this tier on the video row next to the exact height, and the
// payload sends it so both sides agree on what the row will say.
export function qualityTierForHeight(height: number): RecordingQuality {
	if (height <= RECORDING_QUALITY_HEIGHT.LOW) return "LOW";
	if (height <= RECORDING_QUALITY_HEIGHT.MEDIUM) return "MEDIUM";
	if (height <= RECORDING_QUALITY_HEIGHT.HIGH) return "HIGH";
	if (height <= RECORDING_QUALITY_HEIGHT["1440"]) return "1440";
	return "BEST";
}

export function isRecordingQuality(value: string): value is RecordingQuality {
	return RecordingQualitySchema.options.includes(value as RecordingQuality);
}

export function recordingQualityValue(
	value: string,
	fallback: RecordingQuality = "HIGH",
): RecordingQuality {
	return isRecordingQuality(value) ? value : fallback;
}

export function isAudioRecording(recordingType: RecordingMode | string) {
	return recordingType === "audio";
}

export function forceH264For(
	recordingType: RecordingMode | string,
	forceH264: boolean,
) {
	return isAudioRecording(recordingType) ? false : forceH264;
}

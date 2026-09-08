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

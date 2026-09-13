import type { z } from "zod";
import { CreateInputSchema } from "@/api/generated/zod";

export type RecordingMode = "video" | "audio";

// Keep every recording control aligned with the generated API validation.
export const RecordingQualitySchema = CreateInputSchema.shape.quality;
export type RecordingQuality = z.infer<typeof RecordingQualitySchema>;

// Numeric enum keys sort first in JavaScript, so display order is explicit.
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

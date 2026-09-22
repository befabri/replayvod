import type { z } from "zod";
import { CreateInputSchema } from "@/api/generated/zod";

export type RecordingMode = "video" | "audio";

export const RecordingQualitySchema = CreateInputSchema.shape.quality;
export type RecordingQuality = z.infer<typeof RecordingQualitySchema>;

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

export function qualityAboveHD(quality: RecordingQuality): boolean {
	return QUALITY_ORDER[quality] < QUALITY_ORDER.HIGH;
}

export const RECORDING_QUALITY_HEIGHT: Record<RecordingQuality, number> = {
	LOW: 480,
	MEDIUM: 720,
	HIGH: 1080,
	"1440": 1440,
	BEST: Number.POSITIVE_INFINITY,
};

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

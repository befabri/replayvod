import type { TFunction } from "i18next";
import {
	isRecordingQuality,
	recordingQualityValue,
} from "@/lib/recording-settings";

export const scheduleQualityValue = recordingQualityValue;

export function scheduleQualityLabel(t: TFunction, quality: string) {
	if (!isRecordingQuality(quality)) return quality;
	return t(`schedules.quality_${quality.toLowerCase()}`);
}

import type { TFunction } from "i18next";
import {
	isRecordingQuality,
	RECORDING_QUALITIES,
	type RecordingQuality,
	recordingQualityValue,
} from "@/lib/recording-settings";

export const SCHEDULE_QUALITIES = RECORDING_QUALITIES;

export type ScheduleQuality = RecordingQuality;

export const isScheduleQuality = isRecordingQuality;
export const scheduleQualityValue = recordingQualityValue;

export function scheduleQualityLabel(
	t: TFunction,
	quality: ScheduleQuality | string,
) {
	if (!isScheduleQuality(quality)) return quality;
	return t(`schedules.quality_${quality.toLowerCase()}`);
}

export function scheduleQualityOptions(t: TFunction) {
	return SCHEDULE_QUALITIES.map((value) => ({
		value,
		label: scheduleQualityLabel(t, value),
	}));
}

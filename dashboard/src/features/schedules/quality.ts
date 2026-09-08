import type { TFunction } from "i18next";
import {
	isRecordingQuality,
	RECORDING_QUALITIES,
	type RecordingQuality,
	recordingQualityValue,
} from "@/lib/recording-settings";

// The schedule forms offer exactly the qualities the API accepts, in the shared
// display order. This module is the i18n layer over that list; the values
// themselves live in lib/recording-settings, derived from the generated schema.
export const SCHEDULE_QUALITIES = RECORDING_QUALITIES;

export type ScheduleQuality = RecordingQuality;

export const isScheduleQuality = isRecordingQuality;
export const scheduleQualityValue = recordingQualityValue;

// Labels follow the value: "BEST" reads schedules.quality_best, "1440" reads
// schedules.quality_1440. A quality added on the server needs its two locale
// entries and nothing else here. An unknown value (a row written by an older
// or newer server) shows raw rather than a missing-key placeholder.
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

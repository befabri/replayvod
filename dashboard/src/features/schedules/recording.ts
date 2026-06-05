import type { TFunction } from "i18next";

export const SCHEDULE_RECORDING_TYPES = ["video", "audio"] as const;

export type ScheduleRecordingType = (typeof SCHEDULE_RECORDING_TYPES)[number];

export function isScheduleRecordingType(
	value: string,
): value is ScheduleRecordingType {
	return SCHEDULE_RECORDING_TYPES.some((type) => type === value);
}

export function scheduleRecordingTypeValue(
	value: string | undefined,
	fallback: ScheduleRecordingType = "video",
): ScheduleRecordingType {
	return value && isScheduleRecordingType(value) ? value : fallback;
}

export function scheduleRecordingTypeLabel(
	t: TFunction,
	recordingType: string,
) {
	if (recordingType === "audio") return t("schedules.mode_audio");
	return t("schedules.mode_video");
}

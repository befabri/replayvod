import type { TFunction } from "i18next";
import type { ScheduleFormValues } from "./schema";

export const SCHEDULE_QUALITIES = [
	"HIGH",
	"MEDIUM",
	"LOW",
] as const satisfies readonly ScheduleFormValues["quality"][];

export type ScheduleQuality = (typeof SCHEDULE_QUALITIES)[number];

export function isScheduleQuality(value: string): value is ScheduleQuality {
	return SCHEDULE_QUALITIES.some((quality) => quality === value);
}

export function scheduleQualityValue(
	value: string,
	fallback: ScheduleQuality = "HIGH",
): ScheduleQuality {
	return isScheduleQuality(value) ? value : fallback;
}

export function scheduleQualityLabel(
	t: TFunction,
	quality: ScheduleQuality | string,
) {
	if (quality === "HIGH") return t("schedules.quality_high");
	if (quality === "MEDIUM") return t("schedules.quality_medium");
	if (quality === "LOW") return t("schedules.quality_low");
	return quality;
}

export function scheduleQualityOptions(t: TFunction) {
	return SCHEDULE_QUALITIES.map((value) => ({
		value,
		label: scheduleQualityLabel(t, value),
	}));
}

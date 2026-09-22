import { useForm } from "@tanstack/react-form";
import { forceH264For } from "@/lib/recording-settings";
import { ScheduleFormSchema, type ScheduleFormValues } from "./schema";

export function useScheduleForm(
	defaultValues: ScheduleFormValues,
	onSubmit: (value: ScheduleFormValues) => Promise<void>,
) {
	return useForm({
		defaultValues,
		validators: { onSubmit: ScheduleFormSchema },
		onSubmit: ({ value }) => onSubmit(value),
	});
}

export type ScheduleFormApi = ReturnType<typeof useScheduleForm>;

export function buildSchedulePayload(value: ScheduleFormValues) {
	return {
		recording_type: value.recording_type,
		quality: value.quality,
		force_h264: forceH264For(value.recording_type, value.force_h264),
		has_min_viewers: value.has_min_viewers,
		min_viewers: value.has_min_viewers ? value.min_viewers : undefined,
		has_categories: value.has_categories,
		has_tags: value.has_tags,
		is_delete_rediff: value.is_delete_rediff,
		time_before_delete: value.is_delete_rediff
			? value.time_before_delete
			: undefined,
		category_ids: value.has_categories ? value.category_ids : [],
		tag_ids: value.has_tags ? value.tag_ids : [],
	};
}

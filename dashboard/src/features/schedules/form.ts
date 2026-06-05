import { useForm } from "@tanstack/react-form";
import { forceH264For } from "@/lib/recording-settings";
import { ScheduleFormSchema, type ScheduleFormValues } from "./schema";

// useScheduleForm centralizes the shared TanStack Form config used by
// both the create and edit forms: the same validator (the picked,
// superRefined schema) and submit plumbing. Returning the form from one
// factory also gives the shared field components (FiltersFieldset,
// RecordingSettingsField) a single concrete, fully-typed form type to depend on
// instead of falling back to loose typing.
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

// buildSchedulePayload maps the form values to the wire fields shared by
// create and update: the gated inputs (min_viewers, time_before_delete,
// category/tag allowlists) are dropped to their "off" value when their
// toggle is disabled, so a stale value the user can't see never reaches
// the server. The caller spreads in the mode-specific fields
// (broadcaster_id + is_disabled on create, id + is_disabled on edit).
//
// Recording settings follow the same audio/H.264 rule as the shared UI and the
// server normalization: stale force_h264 is cleared when audio is selected.
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

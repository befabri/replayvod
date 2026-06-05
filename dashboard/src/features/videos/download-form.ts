import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { forceH264For } from "@/lib/recording-settings";

// DirectDownloadFormSchema is a client-side narrowing of the tRPC
// TriggerDownloadInputSchema: the generated schema accepts empty strings
// (`z.literal("")`) for backward compat, but the form only ever produces
// the enum values, so we tighten here. broadcaster_id is supplied by the
// caller, not the form, and lives outside the schema.
export const DirectDownloadFormSchema = z.object({
	recording_type: z.enum(["video", "audio"]),
	quality: z.enum(["LOW", "MEDIUM", "HIGH"]),
	force_h264: z.boolean(),
});

export type DirectDownloadFormValues = z.infer<typeof DirectDownloadFormSchema>;

// useDirectDownloadForm centralizes the shared TanStack Form config for
// the "download now" surface so the standalone TriggerDownloadDialog and
// the tabbed ChannelDownloadDialog build the exact same form (same
// defaults, same validator). Returning it from one factory also gives the
// shared DirectDownloadFields component a single concrete, fully-typed
// form to depend on. Mirrors the schedule feature's useScheduleForm.
export function useDirectDownloadForm(
	onSubmit: (value: DirectDownloadFormValues) => Promise<void>,
) {
	return useForm({
		defaultValues: {
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
		} as DirectDownloadFormValues,
		validators: { onSubmit: DirectDownloadFormSchema },
		onSubmit: ({ value }) => onSubmit(value),
	});
}

export type DirectDownloadFormApi = ReturnType<typeof useDirectDownloadForm>;

// buildDirectDownloadPayload shapes the video.triggerDownload wire payload from
// the form values. Force H.264 is video-only, so clear stale checked state for
// audio here as well as in the UI; the server normalizes the same rule.
export function buildDirectDownloadPayload(
	broadcasterId: string,
	value: DirectDownloadFormValues,
) {
	return {
		broadcaster_id: broadcasterId,
		recording_type: value.recording_type,
		quality: value.quality,
		force_h264: forceH264For(value.recording_type, value.force_h264),
	};
}

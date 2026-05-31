import { z } from "zod";

// RecordingWebhookFormSchema validates the editable webhook fields. Events are
// modeled as two booleans (the UI shows one checkbox each) rather than the wire
// array, so buildPayload maps them back to identifiers on submit. The
// superRefine mirrors the server rule: an enabled webhook must target at least
// one event (an enabled webhook with no events is rejected, not treated as
// "all"), which Zod's per-field optionality cannot express on its own.
export const RecordingWebhookFormSchema = z
	.object({
		enabled: z.boolean(),
		url: z.string(),
		onCompleted: z.boolean(),
		onFailed: z.boolean(),
	})
	.superRefine((value, ctx) => {
		if (value.enabled && !value.onCompleted && !value.onFailed) {
			ctx.addIssue({
				code: z.ZodIssueCode.custom,
				path: ["onCompleted"],
				message: "at least one event is required when enabled",
			});
		}
	});

export type RecordingWebhookFormValues = z.infer<
	typeof RecordingWebhookFormSchema
>;

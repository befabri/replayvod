import { z } from "zod";

// RecordingWebhookFormSchema validates the editable webhook fields. Events are
// modeled as two booleans (the UI shows one checkbox each) rather than the wire
// array, so buildPayload maps them back to identifiers on submit. The
// superRefine requires at least one event to stay selected: the server reads an
// empty list as "all events", so an empty selection can't be persisted as-is
// without the saved state silently flipping back to "all" on the next load.
export const RecordingWebhookFormSchema = z
	.object({
		enabled: z.boolean(),
		// Allow an empty URL (a fresh, unconfigured webhook), but validate the
		// format of anything non-empty so a typo is caught client-side instead
		// of only being rejected by the server on submit.
		url: z.union([z.literal(""), z.url()]),
		onCompleted: z.boolean(),
		onFailed: z.boolean(),
	})
	.superRefine((value, ctx) => {
		// At least one event must stay selected, whether or not the webhook is
		// enabled. buildPayload sends only the checked events and the server
		// treats an empty list as "all events", so saving with both unchecked
		// would round-trip back to both-checked on reload. Blocking it keeps
		// the saved state matching what the user left on screen.
		if (!value.onCompleted && !value.onFailed) {
			ctx.addIssue({
				code: z.ZodIssueCode.custom,
				path: ["onCompleted"],
				message: "at least one event is required",
			});
		}
	});

export type RecordingWebhookFormValues = z.infer<
	typeof RecordingWebhookFormSchema
>;

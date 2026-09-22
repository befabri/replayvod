import { z } from "zod";

export const RecordingWebhookFormSchema = z
	.object({
		enabled: z.boolean(),
		url: z.union([z.literal(""), z.url()]),
		onCompleted: z.boolean(),
		onFailed: z.boolean(),
	})
	.superRefine((value, ctx) => {
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

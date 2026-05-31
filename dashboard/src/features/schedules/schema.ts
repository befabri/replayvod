import { z } from "zod";
import { CreateInputSchema } from "@/api/generated/zod";

export const MAX_RETENTION_WINDOW_HOURS = 2562047;

// ScheduleFormSchema picks the fields the create/edit forms expose.
// All filter dimensions from the backend CreateInput are live now:
// min_viewers, category allowlist, tag allowlist.
//
// superRefine adds the conditional requirements on min_viewers and
// time_before_delete — Zod's optional() isn't enough when toggles flip
// requirements on/off at runtime.
export const ScheduleFormSchema = CreateInputSchema.pick({
	broadcaster_id: true,
	quality: true,
	has_min_viewers: true,
	min_viewers: true,
	has_categories: true,
	category_ids: true,
	has_tags: true,
	tag_ids: true,
	is_delete_rediff: true,
	time_before_delete: true,
})
	.extend({
		broadcaster_id: z
			.string()
			.min(1)
			.regex(/^\d+$/, "broadcaster_id must be numeric"),
		// The generated schema still carries the backend's historical
		// unconditional bounds. The service validates this field only when
		// retention is enabled, and the form drops stale values on submit when the
		// toggle is off, so keep this field structurally optional here and enforce
		// the conditional rule below.
		time_before_delete: z.number().optional(),
	})
	.superRefine((value, ctx) => {
		if (value.has_min_viewers && value.min_viewers == null) {
			ctx.addIssue({
				code: z.ZodIssueCode.custom,
				path: ["min_viewers"],
				message: "min_viewers is required when has_min_viewers is enabled",
			});
		}
		if (value.is_delete_rediff) {
			if (value.time_before_delete == null) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["time_before_delete"],
					message:
						"time_before_delete is required when is_delete_rediff is enabled",
				});
			} else if (
				// The backend stores hours as an int64, so a fractional value is
				// rejected at JSON decode. The generated zod only carries gte/lte
				// (no .int()), so enforce whole numbers here for parity.
				!Number.isInteger(value.time_before_delete) ||
				value.time_before_delete < 1 ||
				value.time_before_delete > MAX_RETENTION_WINDOW_HOURS
			) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["time_before_delete"],
					message: `time_before_delete must be a whole number of hours between 1 and ${MAX_RETENTION_WINDOW_HOURS}`,
				});
			}
		}
	});

export type ScheduleFormValues = z.infer<typeof ScheduleFormSchema>;

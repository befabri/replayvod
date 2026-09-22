import { z } from "zod";
import { CreateInputSchema } from "@/api/generated/zod";

export const MAX_RETENTION_WINDOW_HOURS = 2562047;

export const ScheduleFormSchema = CreateInputSchema.pick({
	broadcaster_id: true,
	recording_type: true,
	quality: true,
	force_h264: true,
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
		recording_type: z.enum(["video", "audio"]),
		force_h264: z.boolean(),
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
		if (value.has_categories) {
			if (value.category_ids.length === 0) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["category_ids"],
					message:
						"category_ids must include at least one category when has_categories is enabled",
				});
			} else if (value.category_ids.some((id) => id.trim().length === 0)) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["category_ids"],
					message: "category_ids cannot include empty category IDs",
				});
			}
		}
		if (value.has_tags) {
			if (value.tag_ids.length === 0) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["tag_ids"],
					message:
						"tag_ids must include at least one tag when has_tags is enabled",
				});
			} else if (value.tag_ids.some((id) => !Number.isInteger(id) || id <= 0)) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ["tag_ids"],
					message: "tag_ids must be positive whole numbers",
				});
			}
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

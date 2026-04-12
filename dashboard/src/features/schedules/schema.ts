import { z } from "zod"
import { CreateInputSchema } from "@/api/generated/zod"

// ScheduleFormSchema picks the fields the create/edit forms expose.
// All filter dimensions from the backend CreateInput are live now:
// min_viewers, category allowlist, tag allowlist.
//
// superRefine adds the conditional-required on min_viewers — Zod's
// optional() isn't enough when the toggle flips the requirement
// on/off at runtime.
export const ScheduleFormSchema = CreateInputSchema.pick({
	broadcaster_id: true,
	quality: true,
	has_min_viewers: true,
	min_viewers: true,
	has_categories: true,
	category_ids: true,
	has_tags: true,
	tag_ids: true,
})
	.extend({
		broadcaster_id: z
			.string()
			.min(1)
			.regex(/^\d+$/, "broadcaster_id must be numeric"),
	})
	.superRefine((value, ctx) => {
		if (value.has_min_viewers && value.min_viewers == null) {
			ctx.addIssue({
				code: z.ZodIssueCode.custom,
				path: ["min_viewers"],
				message: "min_viewers is required when has_min_viewers is enabled",
			})
		}
	})

export type ScheduleFormValues = z.infer<typeof ScheduleFormSchema>

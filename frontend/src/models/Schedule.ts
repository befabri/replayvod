import { z } from "zod";

export const ScheduleSchema = z.object({
    channelName: z.string().trim().min(3, { message: "Must be 3 or more characters long" }),
    timeBeforeDelete: z
        .any()
        .transform((val) => Number(val))
        .refine((val) => Number.isInteger(val) && val > 0 && val >= 10, {
            message: "Must be an integer, positive, and a minimum of 10",
        })
        .optional(),
    viewersCount: z
        .any()
        .transform((val) => Number(val))
        .refine((val) => Number.isInteger(val) && val >= 0, {
            message: "Must be an integer and positive or equal to 0",
        })
        .optional(),
    categories: z
        .array(z.string().trim().min(2, { message: "Categories must be at least 2 characters long" }))
        .optional(),
    tags: z.array(z.string().trim().min(2, { message: "Tag must be at least 2 characters long" })).optional(),
    quality: z.enum(["480", "720", "1080"]).optional(),
    isDeleteRediff: z.boolean().default(false),
    hasTags: z.boolean().default(false),
    hasMinView: z.boolean().default(false),
    hasCategory: z.boolean().default(false),
});

export type ScheduleForm = z.infer<typeof ScheduleSchema>;

import { z } from "zod";
import { Quality } from "../type";

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
    category: z.string().trim().min(2, { message: "Must be 2 or more characters long" }),
    tag: z
        .string()
        .regex(/^[a-zA-Z]{2,}(,[a-zA-Z]{2,})*$/, { message: "Invalid tag format" })
        .or(z.literal(""))
        .optional(),
    quality: z.enum([Quality.LOW, Quality.MEDIUM, Quality.HIGH]),
    isDeleteRediff: z.boolean().default(false),
    hasTags: z.boolean().default(false),
    hasMinView: z.boolean().default(false),
    hasCategory: z.boolean().default(false),
});

export type ScheduleForm = z.infer<typeof ScheduleSchema>;

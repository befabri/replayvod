import { z } from "zod";
import { DateTimeFormats } from "../type";
import { timeZones } from "../utils/timezones";

export const SettingsSchema = z.object({
    timeZone: z.enum([...timeZones] as [string, ...string[]]),
    dateTimeFormat: z.enum([...DateTimeFormats] as [string, ...string[]]),
});

export type SettingsForm = z.infer<typeof SettingsSchema>;

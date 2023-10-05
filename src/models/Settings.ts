import { z } from "zod";
import moment from "moment-timezone";
import { DateTimeFormats } from "../type";

const timeZones = moment.tz.names();

export const SettingsSchema = z.object({
    timeZone: z.enum([...timeZones] as [string, ...string[]]),
    dateTimeFormat: z.enum([...DateTimeFormats] as [string, ...string[]]),
});

export type SettingsForm = z.infer<typeof SettingsSchema>;

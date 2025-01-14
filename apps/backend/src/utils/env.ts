import { z } from "zod";

export const envSchema = z
    .object({
        TWITCH_CLIENT_ID: z.string().min(1),
        TWITCH_SECRET: z.string().min(1),
        HMAC_SECRET: z.string().min(1),
        NODE_ENV: z
            .string()
            .transform((val) => val.toLowerCase())
            .refine(
                (val) => val === "dev" || val === "production",
                "Invalid NODE_ENV value. Expected 'dev' or 'production'."
            ),

        WHITELISTED_USER_IDS: z.string().optional(),
        IS_WHITELIST_ENABLED: z
            .string()
            .transform((val) => val.toLowerCase())
            .refine(
                (val) => val === "true" || val === "false",
                "IS_WHITELIST_ENABLED must be 'true', 'false', 'True', or 'False'."
            ),
        CALLBACK_URL: z.string().min(1),
        REACT_URL: z.string().min(1),
        CALLBACK_URL_WEBHOOK: z.string().min(1),
        DATABASE_URL: z.string().min(1),
    })
    .refine((data) => !(data.IS_WHITELIST_ENABLED && !data.WHITELISTED_USER_IDS), {
        message: "WHITELISTED_USER_IDS cannot be empty when IS_WHITELIST_ENABLED is true.",
    })
    .transform((object) => ({
        twitchClientId: object.TWITCH_CLIENT_ID,
        twitchSecret: object.TWITCH_SECRET,
        hmacSecret: object.HMAC_SECRET,
        nodeEnv: object.NODE_ENV,
        whitelistedUserIds: object.WHITELISTED_USER_IDS ? object.WHITELISTED_USER_IDS.split(",") : [],
        isWhitelistEnabled: object.IS_WHITELIST_ENABLED === "true",
        callbackUrl: object.CALLBACK_URL,
        reactUrl: object.REACT_URL,
        callbackUrlWebhook: object.CALLBACK_URL_WEBHOOK,
        databaseUrl: object.DATABASE_URL,
    }));

export type EnvType = z.infer<typeof envSchema>;

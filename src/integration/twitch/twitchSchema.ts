import { z } from "zod";

const ConditionSchema = z.object({
    broadcaster_user_id: z.string().trim().min(1).optional(),
    user_id: z.string().trim().min(1).optional(),
});

const TransportSchema = z.object({
    method: z.string().trim().min(1),
    callback: z.string().trim().min(1),
});

const PaginationSchema = z.object({}).catchall(z.any());

const DataSchema = z.object({
    id: z.string().trim().min(1),
    status: z.string().trim().min(1),
    type: z.string().trim().min(1),
    version: z.string().trim().min(1),
    condition: ConditionSchema,
    created_at: z.string().trim().min(1),
    transport: TransportSchema,
    cost: z.number(),
});

export type EventSubDataSchemaType = z.infer<typeof DataSchema>;

export const EventSubSchema = z.object({
    total: z.number(),
    data: z.array(DataSchema),
    total_cost: z.number(),
    max_total_cost: z.number(),
    pagination: PaginationSchema,
});

export type EventSubType = z.infer<typeof EventSubSchema>;

export const StreamSchema = z.object({
    id: z.string(),
    user_id: z.string(),
    user_login: z.string(),
    user_name: z.string(),
    game_id: z.string(),
    game_name: z.string(),
    type: z.string().optional().default(""),
    title: z.string(),
    viewer_count: z.number(),
    started_at: z.string(),
    language: z.string(),
    thumbnail_url: z.string(),
    tag_ids: z.array(z.string()).optional(),
    tags: z.array(z.string()),
    is_mature: z.boolean().optional(),
});

export type StreamType = z.infer<typeof StreamSchema>;

export const StreamArraySchema = z.array(StreamSchema);

export type StreamArrayType = z.infer<typeof StreamArraySchema>;

export const GameSchema = z.object({
    id: z.string(),
    name: z.string(),
    box_art_url: z.string(),
    igdb_id: z.string(),
});

export const GameArraySchema = z.array(GameSchema);

export type GameArrayType = z.infer<typeof GameArraySchema>;

export type GameType = z.infer<typeof GameSchema>;

export const UserSchema = z.object({
    id: z.string(),
    login: z.string(),
    display_name: z.string(),
    type: z.enum(["admin", "global_mod", "staff", ""]).optional(),
    broadcaster_type: z.enum(["affiliate", "partner", ""]).optional(),
    description: z.string(),
    profile_image_url: z.string(),
    offline_image_url: z.string(),
    view_count: z.number().optional(),
    email: z.string().optional(),
    created_at: z.string(),
});

export const UserArraySchema = z.array(UserSchema);

export type UserArrayType = z.infer<typeof UserArraySchema>;

export type UserType = z.infer<typeof UserSchema>;

export const FollowedChannelSchema = z.object({
    broadcaster_id: z.string(),
    broadcaster_login: z.string(),
    broadcaster_name: z.string(),
    followed_at: z.string(),
});

export const FollowedChannelArraySchema = z.array(FollowedChannelSchema);

export type FollowedChannelArrayType = z.infer<typeof FollowedChannelArraySchema>;

export type FollowedChannelType = z.infer<typeof FollowedChannelSchema>;

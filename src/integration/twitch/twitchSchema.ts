import { z } from "zod";

const ConditionSchema = z.object({
    broadcaster_user_id: z.string().trim().min(1).optional(),
    user_id: z.string().trim().min(1).optional(),
});

const TransportSchema = z.object({
    method: z.string().trim().min(1),
    callback: z.string().trim().min(1),
});

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

const PaginationSchema = z.object({}).catchall(z.any());

export const EventSubSchema = z.object({
    total: z.number(),
    data: z.array(DataSchema),
    total_cost: z.number(),
    max_total_cost: z.number(),
    pagination: PaginationSchema,
});

import z from "zod";
import { Game, User, Stream, FollowedChannel } from "../../models/twitchModel";
import { EventSubSchema } from "./twitchSchema";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "twitch", service: "validation" });

export const isValidStream = (data: any): data is Stream => {
    return (
        typeof data.id === "string" &&
        typeof data.user_id === "string" &&
        typeof data.user_login === "string" &&
        typeof data.user_name === "string" &&
        typeof data.game_id === "string" &&
        typeof data.game_name === "string" &&
        typeof data.type === "string" &&
        typeof data.title === "string" &&
        Array.isArray(data.tags) &&
        typeof data.viewer_count === "number" &&
        typeof data.started_at === "string" &&
        typeof data.language === "string" &&
        typeof data.thumbnail_url === "string" &&
        Array.isArray(data.tag_ids) &&
        (typeof data.is_mature === "boolean" || data.is_mature === undefined)
    );
};

export const isValidGame = (data: any): data is Game => {
    return (
        typeof data.id === "string" &&
        typeof data.name === "string" &&
        typeof data.box_art_url === "string" &&
        typeof data.igdb_id === "string"
    );
};

export const isValidUser = (data: any): data is User => {
    return (
        typeof data.id === "string" &&
        typeof data.login === "string" &&
        typeof data.display_name === "string" &&
        typeof data.type === "string" &&
        (typeof data.broadcaster_type === "string" || data.broadcaster_type === undefined) &&
        typeof data.description === "string" &&
        typeof data.profile_image_url === "string" &&
        typeof data.offline_image_url === "string" &&
        typeof data.view_count === "number" &&
        (typeof data.email === "string" || data.email === undefined) &&
        typeof data.created_at === "string"
    );
};

export const isValidFollowedChannel = (data: any): data is FollowedChannel => {
    return (
        typeof data.broadcaster_id === "string" &&
        typeof data.broadcaster_login === "string" &&
        typeof data.broadcaster_name === "string" &&
        typeof data.followed_at === "string"
    );
};

export const isValidEventSubResponse = (data: any): typeof EventSubSchema._output | null => {
    try {
        const parsedData = EventSubSchema.parse(data);
        return parsedData;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
            if (data?.data) {
                logger.info(data.data);
            }
        } else {
            logger.error("Unexpected error: %s", error);
        }
        return null;
    }
};

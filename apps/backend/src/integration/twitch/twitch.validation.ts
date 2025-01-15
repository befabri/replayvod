import z from "zod";
import {
    EventSubSchema,
    EventSubType,
    FollowedChannelArraySchema,
    FollowedChannelArrayType,
    GameArraySchema,
    GameArrayType,
    GameSchema,
    GameType,
    StreamArraySchema,
    StreamArrayType,
    StreamSchema,
    StreamType,
    UserArraySchema,
    UserArrayType,
    UserSchema,
    UserType,
} from "./twitch.schema";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "twitch", service: "validation" });

export const isValidFollowedChannel = (data: FollowedChannelArrayType): boolean => {
    try {
        FollowedChannelArraySchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'followedChannel' validation: %s", error);
        }
        return false;
    }
};

export const isValidUser = (data: UserType): boolean => {
    try {
        UserSchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'user' validation: %s", error);
        }
        return false;
    }
};

export const isValidUsers = (data: UserArrayType): boolean => {
    try {
        UserArraySchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'users' validation: %s", error);
        }
        return false;
    }
};

export const isValidGame = (data: GameType): boolean => {
    try {
        GameSchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'game' validation: %s", error);
        }
        return false;
    }
};

export const isValidGames = (data: GameArrayType): boolean => {
    try {
        GameArraySchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'games' validation: %s", error);
        }
        return false;
    }
};

export const isValidEventSub = (data: EventSubType): boolean => {
    try {
        EventSubSchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'eventSub' validation: %s", error);
        }
        return false;
    }
};

export const isValidStream = (data: StreamType): boolean => {
    try {
        StreamSchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'stream' validation: %s", error);
        }
        return false;
    }
};

export const isValidStreams = (data: StreamArrayType): boolean => {
    try {
        StreamArraySchema.parse(data);
        return true;
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'streams' validation: %s", error);
        }
        return false;
    }
};

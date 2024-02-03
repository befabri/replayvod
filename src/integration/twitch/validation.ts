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
} from "./twitchSchema";
import { logger as rootLogger } from "../../app";
import {
    EventSubResponse,
    FollowedChannelResponse,
    GameResponse,
    StreamResponse,
    UserResponse,
} from "../../models/twitchModel";
const logger = rootLogger.child({ domain: "twitch", service: "validation" });

export const isValidFollowedChannel = (data: FollowedChannelResponse[]): FollowedChannelArrayType | null => {
    try {
        return FollowedChannelArraySchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'followedChannel' validation: %s", error);
        }
        return null;
    }
};

export const isValidUser = (data: UserResponse): UserType | null => {
    try {
        return UserSchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'user' validation: %s", error);
        }
        return null;
    }
};

export const isValidUsers = (data: UserResponse[]): UserArrayType | null => {
    try {
        return UserArraySchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'users' validation: %s", error);
        }
        return null;
    }
};

export const isValidGame = (data: GameResponse): GameType | null => {
    try {
        return GameSchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'game' validation: %s", error);
        }
        return null;
    }
};

export const isValidGames = (data: GameResponse[]): GameArrayType | null => {
    try {
        return GameArraySchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'games' validation: %s", error);
        }
        return null;
    }
};

export const isValidEventSub = (data: EventSubResponse): EventSubType | null => {
    try {
        return EventSubSchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'eventSub' validation: %s", error);
        }
        return null;
    }
};

export const isValidStream = (data: StreamResponse): StreamType | null => {
    try {
        return StreamSchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'stream' validation: %s", error);
        }
        return null;
    }
};

export const isValidStreams = (data: StreamResponse[]): StreamArrayType | null => {
    try {
        return StreamArraySchema.parse(data);
    } catch (error) {
        if (error instanceof z.ZodError) {
            logger.error("Validation error: %s", error);
        } else {
            logger.error("Unexpected error during 'streams' validation: %s", error);
        }
        return null;
    }
};

import { Category, Channel, UserFollowedChannels, Stream, Subscription } from "@prisma/client";
import {
    FollowedChannel as TwitchFollowedChannel,
    FollowedStream as TwitchFollowedStream,
    Game as TwitchGame,
    User as TwitchUser,
    Stream as TwitchStream,
    EventSubData as TwitchEventSubData,
    EventSubResponse as TwitchEventSubResponse,
    EventSubMeta,
} from "../models/twitchModel";
import { categoryService, channelService, tagService } from "../services";
import { logger as rootLogger } from "../app";

const logger = rootLogger.child({ service: "channelService" });

export const transformTwitchUser = (user: TwitchUser): Channel => {
    return {
        broadcasterId: user.id,
        broadcasterLogin: user.login,
        broadcasterName: user.display_name,
        displayName: user.display_name,
        broadcasterType: user.broadcaster_type,
        createdAt: new Date(user.created_at),
        description: user.description,
        offlineImageUrl: user.offline_image_url,
        profileImageUrl: user.profile_image_url,
        profilePicture: user.profile_image_url,
        type: user.type || null,
        viewCount: user.view_count || 0,
    };
};

export const transformFollowedChannel = async (
    channel: TwitchFollowedChannel,
    userId: string
): Promise<UserFollowedChannels> => {
    const transformedChannel = {
        broadcasterId: channel.broadcaster_id,
        userId: userId,
        followed: true,
        followedAt: new Date(channel.followed_at),
    };
    try {
        if (!(await channelService.channelExists(channel.broadcaster_id))) {
            await channelService.updateChannelDetail(channel.broadcaster_id);
        }
    } catch (error) {
        logger.error(`Error transforming followed channel: ${error.message}`);
        throw error;
    }
    return transformedChannel;
};

export const transformFollowedStream = async (stream: TwitchFollowedStream): Promise<Stream> => {
    const transformedStream = {
        id: stream.id,
        fetchId: "",
        fetchedAt: new Date(),
        isMature: false,
        language: stream.language,
        startedAt: new Date(stream.started_at),
        thumbnailUrl: stream.thumbnail_url,
        title: stream.title,
        type: stream.type,
        broadcasterId: stream.user_id,
        viewerCount: stream.viewer_count,
        tags: [],
        categories: [],
    };
    try {
        if (stream.tags && stream.tags.length > 0) {
            await tagService.addAllTags(stream.tags.map((tagName) => ({ name: tagName })));
            transformedStream.tags = stream.tags.map((tagName) => ({
                tagId: tagName,
                streamId: stream.id,
            }));
        }
        if (stream.game_id) {
            const category = {
                id: stream.game_id,
                name: stream.game_name,
                boxArtUrl: "",
                igdbId: "",
            };
            await categoryService.addCategory(category);
            transformedStream.categories = [
                {
                    categoryId: stream.game_id,
                    streamId: stream.id,
                },
            ];
        }
        if (!(await channelService.channelExists(stream.user_id))) {
            await channelService.updateChannelDetail(stream.user_id);
        }
    } catch (error) {
        logger.error(`Error transforming followed stream: ${error.message}`);
        throw error;
    }
    return transformedStream;
};

export const transformStream = async (stream: TwitchStream): Promise<Stream> => {
    const transformedStream = {
        id: stream.id,
        fetchId: "",
        fetchedAt: new Date(),
        isMature: stream.is_mature,
        language: stream.language,
        startedAt: new Date(stream.started_at),
        thumbnailUrl: stream.thumbnail_url,
        title: stream.title,
        type: stream.type,
        broadcasterId: stream.user_id,
        viewerCount: stream.viewer_count,
        tags: [],
        categories: [],
    };
    try {
        if (stream.tags && stream.tags.length > 0) {
            await tagService.addAllTags(stream.tags.map((tagName) => ({ name: tagName })));
            transformedStream.tags = stream.tags.map((tagName) => ({
                tagId: tagName,
                streamId: stream.id,
            }));
        }
        if (stream.game_id) {
            const category = {
                id: stream.game_id,
                name: stream.game_name,
                boxArtUrl: "",
                igdbId: "",
            };
            await categoryService.addCategory(category);
            transformedStream.categories = [
                {
                    categoryId: stream.game_id,
                    streamId: stream.id,
                },
            ];
        }
        if (!(await channelService.channelExists(stream.user_id))) {
            await channelService.updateChannelDetail(stream.user_id);
        }
    } catch (error) {
        logger.error(`Error transforming stream: ${error.message}`);
        throw error;
    }
    return transformedStream;
};

    export const transformCategory = (game: TwitchGame): Category => {
        return {
            id: game.id,
            boxArtUrl: game.box_art_url,
            igdbId: game.igdb_id || null,
            name: game.name,
        };
    };

export const transformEventSub = (eventSub: TwitchEventSubData): Subscription => {
    const userId = eventSub.condition.broadcaster_user_id || eventSub.condition.user_id || "";

    return {
        id: eventSub.id,
        status: eventSub.status,
        subscriptionType: eventSub.type,
        broadcasterId: userId,
        createdAt: new Date(eventSub.created_at),
        cost: eventSub.cost,
    };
};

export const transformEventSubMeta = (eventSubResponse: TwitchEventSubResponse): EventSubMeta => {
    return {
        total: eventSubResponse.total,
        total_cost: eventSubResponse.total_cost,
        max_total_cost: eventSubResponse.max_total_cost,
    };
};

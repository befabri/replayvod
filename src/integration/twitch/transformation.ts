import { Category, Channel, UserFollowedChannels, Stream, Subscription, User, Tag, Title } from "@prisma/client";
import {
    FollowedChannel as TwitchFollowedChannel,
    Game as TwitchGame,
    User as TwitchUser,
    Stream as TwitchStream,
    EventSubData as TwitchEventSubData,
    EventSubResponse as TwitchEventSubResponse,
    EventSubMeta,
} from "../../models/twitchModel";
import { tagService, titleService } from "../../services";
import * as categoryService from "../../api/category";
import * as channelService from "../../api/channel";
import { logger as rootLogger } from "../../app";

const logger = rootLogger.child({ domain: "twitch", service: "transformUtils" });

export const transformTwitchUser = (user: TwitchUser): Channel => {
    return {
        broadcasterId: user.id,
        broadcasterLogin: user.login,
        broadcasterName: user.display_name,
        displayName: user.display_name,
        broadcasterType: user.broadcaster_type || "",
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

export const transformStream = async (
    stream: TwitchStream
): Promise<{ stream: Stream; tags: Tag[]; category: Category; title: Title }> => {
    const transformedStream = {
        id: stream.id,
        fetchId: "",
        isMature: stream.is_mature || false,
        language: stream.language,
        startedAt: new Date(stream.started_at),
        thumbnailUrl: stream.thumbnail_url,
        type: stream.type,
        broadcasterId: stream.user_id,
        viewerCount: stream.viewer_count,
    };
    const tagsToReturn: Tag[] = [];
    let categoryToReturn: Category = null;
    let titleToReturn = null;
    try {
        if (stream.tags && stream.tags.length > 0) {
            const addedTags = await tagService.addAllTags(stream.tags.map((tagName) => ({ name: tagName })));
            tagsToReturn.push(...addedTags);
        }
        if (stream.game_id) {
            const category = {
                id: stream.game_id,
                name: stream.game_name,
                boxArtUrl: "",
                igdbId: "",
            };
            categoryToReturn = await categoryService.addCategory(category);
        }
        if (stream.title) {
            const title = {
                name: stream.title,
            };
            titleToReturn = await titleService.addTitle(title);
        }
        if (!(await channelService.channelExists(stream.user_id))) {
            await channelService.updateChannelDetail(stream.user_id);
        }
    } catch (error) {
        logger.error(`Error transforming stream: ${error.message}`);
        throw error;
    }

    return {
        stream: transformedStream,
        tags: tagsToReturn,
        category: categoryToReturn,
        title: titleToReturn,
    };
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

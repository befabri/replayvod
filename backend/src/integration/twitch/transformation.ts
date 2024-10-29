import { Category, Channel, UserFollowedChannels, Stream, Subscription, Tag, Title } from "@prisma/client";
import { EventSubDataSchemaType, EventSubMetaType, FollowedChannelType, GameType, StreamType, UserType } from "./twitchSchema";

export const transformTwitchUser = (user: UserType): Channel => {
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

export const transformFollowedChannel = (channel: FollowedChannelType, userId: string): UserFollowedChannels => {
    const transformedChannel = {
        broadcasterId: channel.broadcaster_id,
        userId: userId,
        followed: true,
        followedAt: new Date(channel.followed_at),
    };
    return transformedChannel;
};

export const transformStream = (
    stream: StreamType
): { stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> } => {
    const transformedStream = {
        id: stream.id,
        fetchId: "",
        isMature: stream.is_mature || false,
        language: stream.language,
        startedAt: new Date(stream.started_at),
        endedAt: null,
        thumbnailUrl: stream.thumbnail_url,
        type: stream.type,
        broadcasterId: stream.user_id,
        viewerCount: stream.viewer_count,
    };
    const transformedCategory = { id: stream.game_id, name: stream.game_name, boxArtUrl: "", igdbId: "" };
    const transformedTitle = {
        name: stream.title,
    };
    const transformedTags = stream.tags.map((tagName) => ({ name: tagName }));
    return {
        stream: transformedStream,
        tags: transformedTags,
        category: transformedCategory,
        title: transformedTitle,
    };
};

export const transformCategory = (game: GameType): Category => {
    return {
        id: game.id,
        boxArtUrl: game.box_art_url,
        igdbId: game.igdb_id || null,
        name: game.name,
    };
};

export const transformEventSub = (eventSub: EventSubDataSchemaType): Subscription => {
    const userId = eventSub.condition.broadcaster_user_id || eventSub.condition.user_id;
    return {
        id: eventSub.id,
        status: eventSub.status,
        subscriptionType: eventSub.type,
        broadcasterId: userId || "",
        createdAt: new Date(eventSub.created_at),
        cost: eventSub.cost,
    };
};

export const transformEventSubMeta = (eventSubResponse: EventSubMetaType): EventSubMetaType => {
    return {
        total: eventSubResponse.total,
        total_cost: eventSubResponse.total_cost,
        max_total_cost: eventSubResponse.max_total_cost,
    };
};

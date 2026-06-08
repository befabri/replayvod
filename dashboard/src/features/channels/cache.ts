import type {
	ChannelResponse,
	ChannelUserStateResponse,
} from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { defineCaches, type EntityPatch, keyHasInput } from "@/lib/query";

// Every cache a channel row lives in.
export function channelCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		list: { path: trpc.channel.list, shape: "array" },
		listPage: { path: trpc.channel.listPage, shape: "infinite" },
		getById: { path: trpc.channel.getById, shape: "single" },
		getByLogin: { path: trpc.channel.getByLogin, shape: "single" },
		search: { path: trpc.channel.search, shape: "array" },
		listFollowed: { path: trpc.channel.listFollowed, shape: "array" },
	});
}

// Update the row's user_state everywhere, and drop it from favorites-only
// lists when it was just un-favorited.
export function channelFavoritePatch(
	broadcasterId: string,
	state: ChannelUserStateResponse,
): EntityPatch<ChannelResponse> {
	return {
		match: (channel) => channel.broadcaster_id === broadcasterId,
		update: (channel) => applyChannelFavorite(channel, state),
		removeFrom: (queryKey, shape) =>
			shape === "infinite" &&
			!state.favorite &&
			keyHasInput(queryKey, "filter", "favorites"),
	};
}

function applyChannelFavorite(
	channel: ChannelResponse,
	state: ChannelUserStateResponse,
): ChannelResponse {
	if (
		channel.user_state?.favorite === state.favorite &&
		channel.user_state?.updated_at === state.updated_at
	) {
		return channel;
	}
	return { ...channel, user_state: state };
}

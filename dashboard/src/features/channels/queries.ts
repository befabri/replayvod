import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type {
	ChannelPageResponse,
	ChannelUserStateResponse,
	SetFavoriteInput,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { videoCaches } from "@/features/videos/cache";
import { invalidateCaches, optimisticWrite, patchEntity } from "@/lib/query";
import { channelCaches, channelFavoritePatch } from "./cache";

export function useChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.list.queryOptions());
}

export type ChannelListFilter = "all" | "downloaded" | "favorites";

export function useInfiniteChannels(
	sort: "name_asc" | "name_desc",
	filter: ChannelListFilter = "all",
) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.channel.listPage.infiniteQueryOptions(
			{ limit: 60, sort, filter },
			{
				getNextPageParam: (lastPage: ChannelPageResponse) =>
					lastPage.next_cursor ?? undefined,
			},
		),
	);
}

export function useFollowedChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.listFollowed.queryOptions());
}

export function useChannel(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.channel.getById.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId },
		),
	);
}

export function useChannelSearch(
	query: string,
	limit = 10,
	options?: { enabled?: boolean },
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.channel.search.queryOptions(
			{ query, limit },
			{ enabled: options?.enabled ?? true },
		),
	);
}

export function useSyncChannel() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const channels = channelCaches(trpc);
	const videos = videoCaches(trpc);
	return useMutation(
		trpc.channel.syncFromTwitch.mutationOptions({
			onSuccess: () => {
				invalidateCaches(queryClient, channels);
				invalidateCaches(queryClient, videos, [
					"listPage",
					"byBroadcaster",
					"byCategory",
					"search",
				]);
			},
		}),
	);
}

export function useSetChannelFavorite() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = channelCaches(trpc);
	return useMutation(
		trpc.channel.setFavorite.mutationOptions(
			optimisticWrite<ChannelUserStateResponse, SetFavoriteInput>(
				queryClient,
				caches,
				{
					apply: (qc, input) =>
						patchEntity(
							qc,
							caches,
							channelFavoritePatch(input.broadcaster_id, {
								favorite: input.favorite,
								updated_at: new Date().toISOString(),
							}),
						),
					applyServer: (qc, state, input) =>
						patchEntity(
							qc,
							caches,
							channelFavoritePatch(input.broadcaster_id, state),
						),
				},
			),
		),
	);
}

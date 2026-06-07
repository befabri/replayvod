import {
	type InfiniteData,
	type QueryClient,
	type QueryKey,
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type {
	ChannelPageResponse,
	ChannelResponse,
	ChannelUserStateResponse,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

export function useChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.list.queryOptions());
}

export type ChannelListFilter = "all" | "downloaded" | "favorites";

// useInfiniteChannels fetches the channel list paginated by name.
// The "live now" tab is still applied client-side in the route using
// useLiveSet; the SSE-backed liveSet is the only source that stays
// fresh as channels go on- and offline.
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
	// Empty query returns all channels up to limit (per backend contract) —
	// that's what powers the combobox's "show all" initial state.
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
	return useMutation(
		trpc.channel.syncFromTwitch.mutationOptions({
			onSuccess: () => {
				invalidateChannelCaches(queryClient, trpc);
				// Video lists render broadcaster_name/profile_image_url
				// denormalized from the channel row, so a sync that
				// updated those fields should refresh visible video
				// pages. activeDownloads is SSE-driven and doesn't
				// need an explicit invalidation.
				queryClient.invalidateQueries({
					queryKey: trpc.video.listPage.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.search.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byBroadcaster.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byCategory.queryKey(),
				});
			},
		}),
	);
}

export function useSetChannelFavorite() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.channel.setFavorite.mutationOptions({
			onMutate: async (input) => {
				await cancelChannelQueries(queryClient, trpc);
				const snapshot = snapshotChannelCaches(queryClient, trpc);
				applyChannelFavoriteToCaches(queryClient, trpc, input.broadcaster_id, {
					favorite: input.favorite,
					updated_at: new Date().toISOString(),
				});
				return snapshot;
			},
			onSuccess: (state, input) => {
				applyChannelFavoriteToCaches(
					queryClient,
					trpc,
					input.broadcaster_id,
					state,
				);
			},
			onError: (_err, _input, snapshot) => {
				if (snapshot) {
					restoreChannelCacheSnapshot(queryClient, snapshot);
				}
			},
			onSettled: () => invalidateChannelCaches(queryClient, trpc),
		}),
	);
}

function invalidateChannelCaches(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
) {
	queryClient.invalidateQueries({
		queryKey: trpc.channel.list.pathKey(),
	});
	queryClient.invalidateQueries({
		queryKey: trpc.channel.listPage.pathKey(),
	});
	queryClient.invalidateQueries({
		queryKey: trpc.channel.getById.pathKey(),
	});
	queryClient.invalidateQueries({
		queryKey: trpc.channel.getByLogin.pathKey(),
	});
	queryClient.invalidateQueries({
		queryKey: trpc.channel.search.pathKey(),
	});
	queryClient.invalidateQueries({
		queryKey: trpc.channel.listFollowed.pathKey(),
	});
}

function channelCacheQueryKeys(trpc: ReturnType<typeof useTRPC>) {
	return [
		trpc.channel.list.pathKey(),
		trpc.channel.listPage.pathKey(),
		trpc.channel.getById.pathKey(),
		trpc.channel.getByLogin.pathKey(),
		trpc.channel.search.pathKey(),
		trpc.channel.listFollowed.pathKey(),
	];
}

async function cancelChannelQueries(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
) {
	await Promise.all(
		channelCacheQueryKeys(trpc).map((queryKey) =>
			queryClient.cancelQueries({ queryKey }),
		),
	);
}

type ChannelCacheSnapshot = {
	arrays: [QueryKey, ChannelResponse[] | undefined][];
	pages: [QueryKey, InfiniteData<ChannelPageResponse> | undefined][];
	singles: [QueryKey, ChannelResponse | undefined][];
};

function snapshotChannelCaches(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
): ChannelCacheSnapshot {
	return {
		arrays: [
			...queryClient.getQueriesData<ChannelResponse[]>({
				queryKey: trpc.channel.list.pathKey(),
			}),
			...queryClient.getQueriesData<ChannelResponse[]>({
				queryKey: trpc.channel.search.pathKey(),
			}),
			...queryClient.getQueriesData<ChannelResponse[]>({
				queryKey: trpc.channel.listFollowed.pathKey(),
			}),
		],
		pages: queryClient.getQueriesData<InfiniteData<ChannelPageResponse>>({
			queryKey: trpc.channel.listPage.pathKey(),
		}),
		singles: [
			...queryClient.getQueriesData<ChannelResponse>({
				queryKey: trpc.channel.getById.pathKey(),
			}),
			...queryClient.getQueriesData<ChannelResponse>({
				queryKey: trpc.channel.getByLogin.pathKey(),
			}),
		],
	};
}

function restoreChannelCacheSnapshot(
	queryClient: QueryClient,
	snapshot: ChannelCacheSnapshot,
) {
	for (const [queryKey, data] of snapshot.arrays) {
		queryClient.setQueryData(queryKey, data);
	}
	for (const [queryKey, data] of snapshot.pages) {
		queryClient.setQueryData(queryKey, data);
	}
	for (const [queryKey, data] of snapshot.singles) {
		queryClient.setQueryData(queryKey, data);
	}
}

function applyChannelFavoriteToCaches(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
	broadcasterId: string,
	state: ChannelUserStateResponse,
) {
	for (const queryKey of [
		trpc.channel.list.pathKey(),
		trpc.channel.search.pathKey(),
		trpc.channel.listFollowed.pathKey(),
	]) {
		queryClient.setQueriesData<ChannelResponse[]>({ queryKey }, (old) =>
			updateChannelArrayFavorite(old, broadcasterId, state),
		);
	}

	for (const queryKey of [
		trpc.channel.getById.pathKey(),
		trpc.channel.getByLogin.pathKey(),
	]) {
		queryClient.setQueriesData<ChannelResponse>({ queryKey }, (old) =>
			updateChannelFavorite(old, broadcasterId, state),
		);
	}

	for (const [queryKey, data] of queryClient.getQueriesData<
		InfiniteData<ChannelPageResponse>
	>({ queryKey: trpc.channel.listPage.pathKey() })) {
		queryClient.setQueryData(
			queryKey,
			updateChannelPagesFavorite(data, broadcasterId, state, {
				removeMatching:
					!state.favorite &&
					queryKeyHasChannelListFilter(queryKey, "favorites"),
			}),
		);
	}
}

export function updateChannelPagesFavorite(
	data: InfiniteData<ChannelPageResponse> | undefined,
	broadcasterId: string,
	state: ChannelUserStateResponse,
	options: { removeMatching: boolean },
): InfiniteData<ChannelPageResponse> | undefined {
	if (!data) return data;
	let changed = false;
	const pages = data.pages.map((page) => {
		if (options.removeMatching) {
			const items = page.items.filter(
				(channel) => channel.broadcaster_id !== broadcasterId,
			);
			if (items.length !== page.items.length) {
				changed = true;
				return { ...page, items };
			}
			return page;
		}
		const items = updateChannelItemsFavorite(page.items, broadcasterId, state);
		if (items !== page.items) {
			changed = true;
			return { ...page, items };
		}
		return page;
	});
	return changed ? { ...data, pages } : data;
}

function updateChannelArrayFavorite(
	channels: ChannelResponse[] | undefined,
	broadcasterId: string,
	state: ChannelUserStateResponse,
): ChannelResponse[] | undefined {
	if (!channels) return channels;
	return updateChannelItemsFavorite(channels, broadcasterId, state);
}

function updateChannelItemsFavorite(
	channels: ChannelResponse[],
	broadcasterId: string,
	state: ChannelUserStateResponse,
): ChannelResponse[] {
	let changed = false;
	const next = channels.map((channel) => {
		const updated =
			channel.broadcaster_id === broadcasterId
				? applyChannelFavorite(channel, state)
				: channel;
		if (updated !== channel) changed = true;
		return updated;
	});
	return changed ? next : channels;
}

function updateChannelFavorite(
	channel: ChannelResponse | undefined,
	broadcasterId: string,
	state: ChannelUserStateResponse,
): ChannelResponse | undefined {
	if (!channel || channel.broadcaster_id !== broadcasterId) return channel;
	return applyChannelFavorite(channel, state);
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

export function queryKeyHasChannelListFilter(
	queryKey: QueryKey,
	filter: ChannelListFilter,
) {
	return queryKey.some((part) => valueHasFilter(part, filter));
}

function valueHasFilter(value: unknown, filter: ChannelListFilter): boolean {
	if (Array.isArray(value)) {
		return value.some((part) => valueHasFilter(part, filter));
	}
	if (!value || typeof value !== "object") {
		return false;
	}
	const record = value as Record<string, unknown>;
	if (record.filter === filter) {
		return true;
	}
	return Object.values(record).some((part) => valueHasFilter(part, filter));
}

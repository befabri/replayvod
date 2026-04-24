import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type { ChannelPageResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

export function useChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.list.queryOptions());
}

// useInfiniteChannels fetches the full channel list paginated by
// name. The "live now" filter is applied client-side in the route
// using useLiveSet, not here — the SSE-backed liveSet is the only
// source that stays fresh as channels go on- and offline, so
// embedding live_only in the server query produced stale results.
export function useInfiniteChannels(sort: "name_asc" | "name_desc") {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.channel.listPage.infiniteQueryOptions(
			{ limit: 60, sort, live_only: false },
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

export function useChannelSearch(query: string, limit = 10) {
	const trpc = useTRPC();
	// Empty query returns all channels up to limit (per backend contract) —
	// that's what powers the combobox's "show all" initial state.
	return useQuery(trpc.channel.search.queryOptions({ query, limit }));
}

export function useSyncChannel() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.channel.syncFromTwitch.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.channel.list.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.channel.listPage.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.channel.getById.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.channel.getByLogin.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.channel.search.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.channel.listFollowed.queryKey(),
				});
				// Video lists render broadcaster_name/profile_image_url
				// denormalized from the channel row, so a sync that
				// updated those fields should refresh visible video
				// pages. activeDownloads is SSE-driven and doesn't
				// need an explicit invalidation.
				queryClient.invalidateQueries({
					queryKey: trpc.video.listPage.queryKey(),
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

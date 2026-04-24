import {
	keepPreviousData,
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useState } from "react";
import type {
	ActiveDownloadResponse,
	VideoListPageCursor,
	VideoListPageResponse,
	VideoPageResponse,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

// VideoCategory is the row shape for a category attached to a video
// recording. `duration_seconds` is the total tracked time the stream
// spent in that category.
export type VideoCategory = {
	id: string;
	name: string;
	box_art_url?: string | null;
	started_at: string;
	ended_at?: string | null;
	duration_seconds: number;
};

export type VideoTitle = {
	id: number;
	name: string;
	started_at: string;
	ended_at?: string | null;
	duration_seconds: number;
};

export type VideoSort = "created_at" | "duration" | "size" | "channel";
export type VideoOrder = "asc" | "desc";
export type VideoListFilters = {
	quality?: string;
	broadcasterId?: string;
	language?: string;
	duration?: string;
	size?: string;
};

export function useInfiniteVideoPages(
	limit = 50,
	status?: string,
	sort?: VideoSort,
	order?: VideoOrder,
	filters?: VideoListFilters,
) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.video.listPage.infiniteQueryOptions(
			{
				limit,
				status: status ?? "",
				sort: sort ?? "",
				order: order ?? "",
				quality: filters?.quality ?? "",
				broadcaster_id: filters?.broadcasterId ?? "",
				language: filters?.language ?? "",
				duration: filters?.duration ?? "",
				size: filters?.size ?? "",
			},
			{
				getNextPageParam: (lastPage: VideoListPageResponse) =>
					lastPage.next_cursor ?? undefined,
				// Keep the previous filter's data mounted while the new
				// query fires. Paired with a client-side narrowing pass
				// in the route, the UI stays populated during the
				// refetch rather than flashing a skeleton on every
				// filter change.
				placeholderData: keepPreviousData,
			},
		),
	);
}

export function useVideoListPage(
	limit = 50,
	status?: string,
	sort?: VideoSort,
	order?: VideoOrder,
	cursor?: VideoListPageCursor,
	filters?: VideoListFilters,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.listPage.queryOptions({
			limit,
			status: status ?? "",
			sort: sort ?? "",
			order: order ?? "",
			quality: filters?.quality ?? "",
			broadcaster_id: filters?.broadcasterId ?? "",
			language: filters?.language ?? "",
			duration: filters?.duration ?? "",
			size: filters?.size ?? "",
			cursor,
		}),
	);
}

export function useVideo(id: number) {
	const trpc = useTRPC();
	return useQuery(trpc.video.getById.queryOptions({ id }, { enabled: id > 0 }));
}

// useVideoTitles fetches the full title history for a video. Empty
// array when title tracking is disabled on the server or the
// recording was too short to capture a change. The UI shows the
// badge only when length > 1 (single title = same info as
// video.title on the VideoResponse).
export function useVideoTitles(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.titles.queryOptions(
			{ video_id: videoId },
			{ enabled: enabled && videoId > 0 },
		),
	);
}

// useVideoCategories fetches the categories seen during a recording.
// A stream can change category mid-broadcast; the server appends each
// distinct category to the M2M via LinkStreamCategory on every live-
// poll tick. Empty array means the recording predates category
// tracking or the stream ran with no recognized category.
export function useVideoCategories(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.categories.queryOptions(
			{ video_id: videoId },
			{ enabled: enabled && videoId > 0 },
		),
	);
}

// useVideoSnapshots returns the ordered list of snapshot storage
// paths captured during a recording (one per live-preview tick).
// The VideoCard's hover effect cycles through these — the backend
// probes storage, so an empty result means either no snapshots
// (title_tracking disabled, too-short recording) or the recording
// predates the snapshotter. `enabled` is a lazy-load gate: list
// queries shouldn't fire for every card, only for ones currently
// under hover.
export function useVideoSnapshots(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.snapshots.queryOptions(
			{ video_id: videoId },
			{
				enabled: enabled && videoId > 0,
				// Storage contents don't change after DONE — we can
				// cache forever for the session. A refresh rebuilds
				// the query when the user reloads the page.
				staleTime: Number.POSITIVE_INFINITY,
			},
		),
	);
}

export function useInfiniteVideosByBroadcaster(
	broadcasterId: string,
	limit = 24,
) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.video.byBroadcaster.infiniteQueryOptions(
			{ broadcaster_id: broadcasterId, limit },
			{
				enabled: !!broadcasterId,
				getNextPageParam: (lastPage: VideoPageResponse) =>
					lastPage.next_cursor ?? undefined,
			},
		),
	);
}

export function useInfiniteVideosByCategory(categoryId: string, limit = 24) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.video.byCategory.infiniteQueryOptions(
			{ category_id: categoryId, limit },
			{
				enabled: !!categoryId,
				getNextPageParam: (lastPage: VideoPageResponse) =>
					lastPage.next_cursor ?? undefined,
			},
		),
	);
}

export function useStatistics() {
	const trpc = useTRPC();
	return useQuery(trpc.video.statistics.queryOptions());
}

export function useActiveDownloads() {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.activeDownloads.queryOptions(undefined, {
			refetchInterval: 2_000,
			staleTime: 1_000,
		}),
	);
}

// useLiveActiveDownloads streams active downloads via the server's
// SSE subscription and mirrors each tick into the tanstack-query
// cache under trpc.video.activeDownloads.queryKey(). Mirroring
// through the cache (rather than a component-local useState) means
// an unmount/remount keeps the last known state, and any other
// consumer reading that key sees the same rows.
//
// enabled: false ensures mutations that invalidate activeDownloads
// never refetch through HTTP and race with live SSE writes — the
// subscription is the sole writer for this key while this hook is
// mounted.
export function useLiveActiveDownloads() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const queryKey = trpc.video.activeDownloads.queryKey();
	const [error, setError] = useState<Error | null>(null);

	const { data } = useQuery(
		trpc.video.activeDownloads.queryOptions(undefined, {
			enabled: false,
			staleTime: Number.POSITIVE_INFINITY,
		}),
	);

	useSubscription({
		...trpc.video.activeDownloadsLive.subscriptionOptions(),
		onData: (rows: ActiveDownloadResponse[]) => {
			queryClient.setQueryData(queryKey, rows);
			setError(null);
		},
		onError: (err: unknown) => {
			setError(err instanceof Error ? err : new Error("subscription failed"));
		},
	});

	return {
		data,
		isLoading: data === undefined && error == null,
		isError: error != null,
		error,
	};
}

export function useTriggerDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.video.triggerDownload.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.video.listPage.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byBroadcaster.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byCategory.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.getById.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.statistics.queryKey(),
				});
			},
		}),
	);
}

export function useCancelDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.video.cancel.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.video.listPage.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byBroadcaster.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.byCategory.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.getById.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.video.statistics.queryKey(),
				});
			},
		}),
	);
}

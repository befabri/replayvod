import {
	keepPreviousData,
	type QueryClient,
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useMemo, useState } from "react";
import type {
	ActiveDownloadResponse,
	TimelineEvent,
	TitleItem,
	VideoCategory,
	VideoListPageCursor,
	VideoListPageResponse,
	VideoPageResponse,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { timelineEventsWithSpanFallback } from "@/features/videos/timeline";
import { withSessionProbe } from "@/stores/auth";

// VideoCategory (the category-span row) and VideoTitle (the title-span row) are
// the generated wire shapes of video.categories / video.titles. VideoTitle
// keeps its dashboard name as an alias of the generated TitleItem.
export type { VideoCategory };
export type VideoTitle = TitleItem;

export type VideoTimelineQueryOptions = {
	refetchInterval?: number;
	staleTime?: number;
};

export type VideoSort = "created_at" | "duration" | "size" | "channel";
export type VideoOrder = "asc" | "desc";
export type VideoListFilters = {
	quality?: string;
	broadcasterId?: string;
	language?: string;
	duration?: string;
	size?: string;
	window?: string;
	incompleteOnly?: boolean;
};

export function useInfiniteVideoPages(
	limit = 50,
	status?: string,
	sort?: VideoSort,
	order?: VideoOrder,
	filters?: VideoListFilters,
	options?: { enabled?: boolean },
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
				window: filters?.window ?? "",
				incomplete_only: filters?.incompleteOnly ?? false,
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
				// Callers gate the query on an explicit flag for tabs
				// that have no backend support (unwatched / favorites)
				// — we render a "coming soon" body without firing
				// `video.listPage` for results that wouldn't be shown.
				enabled: options?.enabled ?? true,
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
			window: filters?.window ?? "",
			incomplete_only: filters?.incompleteOnly ?? false,
			cursor,
		}),
	);
}

export function useVideoSearch(
	query: string,
	limit = 8,
	options?: { enabled?: boolean },
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.search.queryOptions(
			{ query, limit },
			{ enabled: options?.enabled ?? true },
		),
	);
}

export function useVideo(id: number) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.getById.queryOptions(
			{ id },
			{
				enabled: id > 0,
				// The single-file playback artifact is built lazily — the first time
				// this recording is played (the server kicks it when a part is
				// streamed). So a multi-part recording opened for the first time has
				// no artifact row yet; it appears as "building" and then "ready" over
				// the next seconds/minutes. Poll while a finished multi-part recording
				// has no ready artifact so the player upgrades from the part sequencer
				// to the continuous file the moment the build lands, then stop.
				//
				// Stop once the artifact reaches any terminal state: "ready" (the
				// player swaps to it), or "failed"/"unavailable" (won't become ready
				// without another play). Keep polling only while it's absent or
				// "building". A recording too big for the cache cap is left with no
				// row, so it keeps polling while its watch page is open — bounded to
				// the session, since the query unmounts on navigate.
				refetchInterval: (query) => {
					const v = query.state.data;
					if (!v || v.status !== "DONE") return false;
					const status = v.playback_artifact?.status;
					if (
						status === "ready" ||
						status === "failed" ||
						status === "unavailable"
					) {
						return false;
					}
					return (v.parts?.length ?? 0) >= 2 ? 4000 : false;
				},
			},
		),
	);
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

// useVideoTimeline fetches the merged title + category change events
// for a recording, ordered chronologically. Each row carries an
// optional title and an optional category; the schema CHECK on
// video_metadata_changes guarantees at least one is present. Empty
// array for recordings predating migration 031.
export function useVideoTimeline(
	videoId: number,
	enabled = true,
	options?: VideoTimelineQueryOptions,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.timeline.queryOptions(
			{ video_id: videoId },
			{
				enabled: enabled && videoId > 0,
				refetchInterval: options?.refetchInterval,
				staleTime: options?.staleTime,
			},
		),
	);
}

export function useMergedTimeline(
	videoId: number,
	enabled = true,
	options?: VideoTimelineQueryOptions,
): {
	data: TimelineEvent[];
	rawEvents: TimelineEvent[] | undefined;
	titleSpans: VideoTitle[] | undefined;
	categorySpans: VideoCategory[] | undefined;
	isLoading: boolean;
} {
	const timelineQuery = useVideoTimeline(videoId, enabled, options);
	const titleSpansQuery = useVideoTitles(videoId, enabled);
	const categorySpansQuery = useVideoCategories(videoId, enabled);
	const data = useMemo(
		() =>
			timelineEventsWithSpanFallback(
				timelineQuery.data,
				titleSpansQuery.data,
				categorySpansQuery.data,
			),
		[timelineQuery.data, titleSpansQuery.data, categorySpansQuery.data],
	);

	return {
		data,
		rawEvents: timelineQuery.data,
		titleSpans: titleSpansQuery.data,
		categorySpans: categorySpansQuery.data,
		isLoading:
			timelineQuery.isLoading ||
			titleSpansQuery.isLoading ||
			categorySpansQuery.isLoading,
	};
}

// useVideoSnapshots returns the ordered list of snapshot storage
// paths captured during a recording (one per live-preview tick).
// The VideoCard's hover effect cycles through these — the backend
// probes storage, so an empty result means either no snapshots
// (server mode off, too-short recording) or the recording
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
	return useQuery(
		trpc.video.statistics.queryOptions(undefined, {
			// Refresh on a slow cadence so server-side row transitions
			// (download completion, hourly cleanups) tick into the
			// dashboard without the user reloading. Mutations that
			// originate from the UI already invalidate this key
			// directly; the interval covers the gap for events the
			// dashboard didn't trigger.
			refetchInterval: 30_000,
			refetchOnWindowFocus: true,
		}),
	);
}

// useChannelStatistics rolls up DONE recordings for one broadcaster:
// total count, summed bytes, summed duration. Backed by a single
// SQL aggregate (no client-side pagination) so the watch page can
// surface a "N recordings · X GB" line without paying for a full
// library scan over tRPC.
export function useChannelStatistics(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.statisticsByBroadcaster.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId, staleTime: 30_000 },
		),
	);
}

// useDownloadCapacity reads the service-wide concurrent-download cap. It's
// static server config, so it's fetched once and kept indefinitely.
export function useDownloadCapacity() {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.downloadCapacity.queryOptions(undefined, {
			staleTime: Number.POSITIVE_INFINITY,
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

	const { data, dataUpdatedAt } = useQuery(
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
		onError: withSessionProbe((err) => {
			setError(err instanceof Error ? err : new Error("subscription failed"));
		}),
	});

	return {
		data,
		// Wall-clock time of the latest SSE sample, so consumers can extrapolate
		// live counters (elapsed clock) forward between pushes.
		dataUpdatedAt,
		isLoading: data === undefined && error == null,
		isError: error != null,
		error,
	};
}

// invalidateVideoCaches refetches every cache a download mutation can
// shift: the paged list, the per-broadcaster and per-category grids,
// the single-video record, and both statistics rollups. triggerDownload
// and cancel both move rows across this set, so it lives in one place
// rather than drifting apart between the two call sites.
function invalidateVideoCaches(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
) {
	for (const queryKey of [
		trpc.video.listPage.queryKey(),
		trpc.video.search.queryKey(),
		trpc.video.byBroadcaster.queryKey(),
		trpc.video.byCategory.queryKey(),
		trpc.video.getById.queryKey(),
		trpc.video.statistics.queryKey(),
		trpc.video.statisticsByBroadcaster.queryKey(),
	]) {
		queryClient.invalidateQueries({ queryKey });
	}
}

export function useTriggerDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.video.triggerDownload.mutationOptions({
			onSuccess: () => invalidateVideoCaches(queryClient, trpc),
		}),
	);
}

export function useCancelDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.video.cancel.mutationOptions({
			onSuccess: () => invalidateVideoCaches(queryClient, trpc),
		}),
	);
}

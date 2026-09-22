import {
	keepPreviousData,
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useCallback, useState } from "react";
import type {
	ActiveDownloadResponse,
	SetWatchLaterInput,
	StatisticsResponse,
	TitleItem,
	VideoCategory,
	VideoListPageResponse,
	VideoPageResponse,
	VideoResponse,
	VideoSource,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { API_URL } from "@/env";
import {
	invalidateCaches,
	optimisticWrite,
	patchEntity,
	resyncQuery,
} from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";
import { VIDEO_LIST_CACHES, videoCaches, videoUserStatePatch } from "./cache";

export type { VideoCategory };
export type VideoTitle = TitleItem;

export type VideoTimelineQueryOptions = {
	refetchInterval?: number;
	staleTime?: number;
};

export type VideoSort =
	| "created_at"
	| "duration"
	| "size"
	| "channel"
	| "history_when"
	| "broadcast_at"
	| "last_watched";
export type VideoOrder = "asc" | "desc";
export type VideoScope = "active" | "removed" | "all";
export type VideoOutcome = "completed" | "failed" | "cancelled";
export type VideoDeletionKind = "retention" | "manual" | "missing";
export type VideoListFilters = {
	quality?: string;
	broadcasterId?: string;
	language?: string;
	source?: VideoSource;
	duration?: string;
	size?: string;
	window?: string;
	incompleteOnly?: boolean;
	watchLaterOnly?: boolean;
	unwatchedOnly?: boolean;
	continueWatchingOnly?: boolean;
	terminalOnly?: boolean;
	scope?: VideoScope;
	outcome?: VideoOutcome;
	deletionKind?: VideoDeletionKind;
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
				source: filters?.source ?? "",
				duration: filters?.duration ?? "",
				size: filters?.size ?? "",
				window: filters?.window ?? "",
				incomplete_only: filters?.incompleteOnly ?? false,
				watch_later_only: filters?.watchLaterOnly ?? false,
				unwatched_only: filters?.unwatchedOnly ?? false,
				continue_watching_only: filters?.continueWatchingOnly ?? false,
				terminal_only: filters?.terminalOnly ?? false,
				scope: filters?.scope ?? "",
				outcome: filters?.outcome ?? "",
				deletion_kind: filters?.deletionKind ?? "",
			},
			{
				getNextPageParam: (lastPage: VideoListPageResponse) =>
					lastPage.next_cursor ?? undefined,
				placeholderData: keepPreviousData,
				enabled: options?.enabled ?? true,
			},
		),
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

export function useRelatedRecordings(id: number) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.relatedRecordings.queryOptions(
			{ id },
			{
				enabled: id > 0,
				placeholderData: (previous) =>
					previous?.items.some((item) => item.id === id) ? previous : undefined,
			},
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
				refetchInterval: (query) => {
					const v = query.state.data;
					if (v?.deleted_at) return false;
					if (v?.status !== "DONE") return false;
					const status = v.playback_artifact?.status;
					if (
						status === "ready" ||
						status === "failed" ||
						status === "unavailable"
					) {
						return false;
					}
					return (v.parts?.length ?? 0) >= 2 ? 4_000 : false;
				},
			},
		),
	);
}

export type AudioWaveform = {
	duration_seconds: number;
	peaks: number[];
};

class RestApiError extends Error {
	data: { httpStatus: number; code?: string };

	constructor(response: Response) {
		super(`HTTP ${response.status}`);
		this.name = "RestApiError";
		this.data = {
			httpStatus: response.status,
			code: response.status === 401 ? "UNAUTHORIZED" : undefined,
		};
	}
}

export function useAudioWaveform(videoId: number, enabled = true) {
	return useQuery<AudioWaveform | null>({
		queryKey: ["video", "audio-waveform", videoId],
		enabled: enabled && videoId > 0,
		staleTime: (query) => (query.state.data ? Number.POSITIVE_INFINITY : 0),
		queryFn: async () => {
			const response = await fetch(
				`${API_URL}/api/v1/videos/${videoId}/waveform`,
				{ credentials: "include" },
			);
			if (response.status === 404) return null;
			if (!response.ok) throw new RestApiError(response);
			return (await response.json()) as AudioWaveform;
		},
	});
}

export function useVideoTitles(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.titles.queryOptions(
			{ video_id: videoId },
			{ enabled: enabled && videoId > 0 },
		),
	);
}

export function useVideoCategories(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.categories.queryOptions(
			{ video_id: videoId },
			{ enabled: enabled && videoId > 0 },
		),
	);
}

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

export function useVideoSnapshots(videoId: number, enabled = true) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.snapshots.queryOptions(
			{ video_id: videoId },
			{
				enabled: enabled && videoId > 0,
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

export function useHistoryCounts() {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.historyCounts.queryOptions(undefined, {
			refetchInterval: 30_000,
			refetchOnWindowFocus: true,
		}),
	);
}

export function useStatistics() {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.statistics.queryOptions(undefined, {
			refetchInterval: 30_000,
			refetchOnWindowFocus: true,
		}),
	);
}

export function useChannelStatistics(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.statisticsByBroadcaster.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId, staleTime: 30_000 },
		),
	);
}

export function useDownloadCapacity() {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.downloadCapacity.queryOptions(undefined, {
			staleTime: Number.POSITIVE_INFINITY,
		}),
	);
}

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
		dataUpdatedAt,
		isLoading: data === undefined && error == null,
		isError: error != null,
		error,
	};
}

export function liveRenditionsOptions(
	trpc: ReturnType<typeof useTRPC>,
	broadcasterId: string,
	forceH264: boolean,
) {
	return trpc.video.liveRenditions.queryOptions(
		{ broadcaster_id: broadcasterId, force_h264: forceH264 },
		{ retry: false, staleTime: 30_000 },
	);
}

export function useTriggerDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return useMutation(
		trpc.video.triggerDownload.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches),
		}),
	);
}

export function useCancelDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return useMutation(
		trpc.video.cancel.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches),
		}),
	);
}

export function useLiveVideoChanges() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const resync = () =>
		Promise.all(
			Object.values(videoCaches(trpc)).map(({ pathKey }) =>
				resyncQuery(queryClient, pathKey),
			),
		);
	useSubscription({
		...trpc.video.changesLive.subscriptionOptions(),
		onStarted: resync,
		onData: resync,
		onError: withSessionProbe(),
	});
}

export function useDeleteVideo() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return useMutation(
		trpc.video.delete.mutationOptions({
			onSuccess: (_result, { id }) => {
				patchEntity<VideoResponse>(queryClient, caches, {
					match: (video) => video.id === id,
					update: (video) => ({
						...video,
						delete_requested_at:
							video.delete_requested_at ?? new Date().toISOString(),
					}),
				});
				invalidateCaches(queryClient, caches);
			},
		}),
	);
}

export function useRestoreVideo() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return useMutation(
		trpc.video.restore.mutationOptions({
			onSuccess: () => {
				invalidateCaches(queryClient, caches);
			},
		}),
	);
}

export function useSetWatchLater() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return useMutation(
		trpc.video.setWatchLater.mutationOptions(
			optimisticWrite<VideoUserStateResponse, SetWatchLaterInput>(
				queryClient,
				caches,
				{
					apply: (qc, { video_id, watch_later }) => {
						const existing = findCachedVideo(qc, caches, video_id);
						const state: VideoUserStateResponse = existing
							? optimisticWatchLaterState(existing, watch_later)
							: {
									watch_later,
									last_position_seconds: 0,
									updated_at: new Date().toISOString(),
								};
						patchEntity(
							qc,
							caches,
							videoUserStatePatch(
								video_id,
								state,
								qc.getQueryData(trpc.settings.get.queryKey())?.playback,
							),
						);
						qc.setQueriesData<StatisticsResponse>(
							{ queryKey: caches.statistics.pathKey },
							(cache) => applyWatchLaterDeltaToStats(cache, watch_later),
						);
					},
					applyServer: (qc, state, { video_id }) =>
						patchEntity(
							qc,
							caches,
							videoUserStatePatch(
								video_id,
								state,
								qc.getQueryData(trpc.settings.get.queryKey())?.playback,
							),
						),
				},
			),
		),
	);
}

export function useContinueWatching(limit = 12) {
	const trpc = useTRPC();
	return useQuery(trpc.video.continueWatching.queryOptions({ limit }));
}

function optimisticWatchLaterState(
	video: VideoResponse,
	watchLater: boolean,
): VideoUserStateResponse {
	return {
		watch_later: watchLater,
		last_position_seconds: video.user_state?.last_position_seconds ?? 0,
		watched_at: video.user_state?.watched_at,
		completed_at: video.user_state?.completed_at,
		updated_at: new Date().toISOString(),
	};
}

function applyWatchLaterDeltaToStats(
	cache: StatisticsResponse | undefined,
	watchLater: boolean,
): StatisticsResponse | undefined {
	if (!cache) return cache;
	const delta = watchLater ? 1 : -1;
	return {
		...cache,
		watch_later: Math.max(0, cache.watch_later + delta),
	};
}

function findCachedVideo(
	queryClient: ReturnType<typeof useQueryClient>,
	caches: ReturnType<typeof videoCaches>,
	videoId: number,
): VideoResponse | undefined {
	for (const spec of Object.values(caches)) {
		if (spec.shape === "scalar") continue;
		for (const [, data] of queryClient.getQueriesData({
			queryKey: spec.pathKey,
		})) {
			const found = findVideoInCachedData(data, videoId);
			if (found) return found;
		}
	}
	return undefined;
}

function findVideoInCachedData(
	data: unknown,
	videoId: number,
): VideoResponse | undefined {
	if (!data) return undefined;
	if (Array.isArray(data)) {
		for (const item of data) {
			const found = findVideoInCachedData(item, videoId);
			if (found) return found;
		}
		return undefined;
	}
	if (typeof data !== "object") return undefined;
	const maybeVideo = data as Partial<VideoResponse>;
	if (maybeVideo.id === videoId && typeof maybeVideo.job_id === "string") {
		return maybeVideo as VideoResponse;
	}
	for (const value of Object.values(data)) {
		const found = findVideoInCachedData(value, videoId);
		if (found) return found;
	}
	return undefined;
}

export function useInvalidateVideo(id: number) {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useCallback(() => {
		invalidateCaches(queryClient, videoCaches(trpc), VIDEO_LIST_CACHES);
		return queryClient.invalidateQueries({
			queryKey: trpc.video.getById.queryKey({ id }),
		});
	}, [id, queryClient, trpc]);
}

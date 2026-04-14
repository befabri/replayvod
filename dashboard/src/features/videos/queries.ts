import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { CategoryResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

// VideoCategory is the row shape for a category attached to a video
// recording. Keeps the visible fields (name, box art) without
// leaking the full CategoryResponse shape.
export type VideoCategory = Pick<
	CategoryResponse,
	"id" | "name" | "box_art_url"
>;

export type VideoSort = "created_at" | "duration" | "size" | "channel";
export type VideoOrder = "asc" | "desc";

export function useVideos(
	limit = 50,
	offset = 0,
	status?: string,
	sort?: VideoSort,
	order?: VideoOrder,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.list.queryOptions({
			limit,
			offset,
			status: status ?? "",
			sort: sort ?? "",
			order: order ?? "",
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

export function useVideosByBroadcaster(
	broadcasterId: string,
	limit = 50,
	offset = 0,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.byBroadcaster.queryOptions(
			{ broadcaster_id: broadcasterId, limit, offset },
			{ enabled: !!broadcasterId },
		),
	);
}

export function useVideosByCategory(
	categoryId: string,
	limit = 50,
	offset = 0,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.video.byCategory.queryOptions(
			{ category_id: categoryId, limit, offset },
			{ enabled: !!categoryId },
		),
	);
}

export function useStatistics() {
	const trpc = useTRPC();
	return useQuery(trpc.video.statistics.queryOptions());
}

export function useTriggerDownload() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.video.triggerDownload.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.video.list.queryKey() });
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
				queryClient.invalidateQueries({ queryKey: trpc.video.list.queryKey() });
			},
		}),
	);
}

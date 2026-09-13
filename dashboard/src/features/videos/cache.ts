import type {
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { defineCaches, type EntityPatch, keyHasInput } from "@/lib/query";
import { isContinueWatchingVideo, type ResumePolicy } from "./resume-policy";

// Every cache a video row lives in, plus derived summaries and aggregates
// (scalar: invalidated, never patched as full video rows).
export function videoCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		listPage: { path: trpc.video.listPage, shape: "infinite" },
		byBroadcaster: { path: trpc.video.byBroadcaster, shape: "infinite" },
		byCategory: { path: trpc.video.byCategory, shape: "infinite" },
		search: { path: trpc.video.search, shape: "array" },
		continueWatching: { path: trpc.video.continueWatching, shape: "array" },
		getById: { path: trpc.video.getById, shape: "single" },
		relatedRecordings: { path: trpc.video.relatedRecordings, shape: "scalar" },
		historyCounts: { path: trpc.video.historyCounts, shape: "scalar" },
		statistics: { path: trpc.video.statistics, shape: "scalar" },
		statisticsByBroadcaster: {
			path: trpc.video.statisticsByBroadcaster,
			shape: "scalar",
		},
	});
}

// The list/aggregate caches a write should refetch without disturbing the
// single-video query (getById) the watch page may be actively polling.
export const VIDEO_LIST_CACHES = [
	"listPage",
	"byBroadcaster",
	"byCategory",
	"search",
	"continueWatching",
	"relatedRecordings",
	"statistics",
	"statisticsByBroadcaster",
] as const;

// Progress changes the library lists and its per-user tab counts. Channel
// statistics only count recordings and bytes, so they stay out of this set.
export const VIDEO_USER_STATE_CACHES = [
	"listPage",
	"byBroadcaster",
	"byCategory",
	"search",
	"continueWatching",
	"statistics",
] as const;

// Merge the new user_state wherever the row appears, and drop it from filter-only
// lists it left (a watch-later list once un-flagged, an unwatched list once watched).
export function videoUserStatePatch(
	videoId: number,
	state: VideoUserStateResponse,
	policy?: ResumePolicy,
): EntityPatch<VideoResponse> {
	return {
		match: (video) => video.id === videoId,
		update: (video) => applyVideoUserState(video, state),
		removeFrom: (queryKey, shape, video) => {
			if (shape === "infinite") {
				return (
					(!video.user_state?.watch_later &&
						keyHasInput(queryKey, "watch_later_only", true)) ||
					(!!video.user_state?.watched_at &&
						keyHasInput(queryKey, "unwatched_only", true)) ||
					(keyHasInput(queryKey, "continue_watching_only", true) &&
						policy != null &&
						!isContinueWatchingVideo(video, policy))
				);
			}
			// This array is the dashboard preview; search arrays keep the row.
			return (
				shape === "array" &&
				Array.isArray(queryKey[0]) &&
				queryKey[0].join(".") === "video.continueWatching" &&
				policy != null &&
				!isContinueWatchingVideo(video, policy)
			);
		},
	};
}

function applyVideoUserState(
	video: VideoResponse,
	state: VideoUserStateResponse,
): VideoResponse {
	const current = video.user_state;
	// Bookmark responses include a progress snapshot that can precede a newer save.
	if (
		current &&
		(current.progress_revision ?? 0) > (state.progress_revision ?? 0)
	) {
		return {
			...video,
			user_state: { ...current, watch_later: state.watch_later },
		};
	}
	return { ...video, user_state: { ...video.user_state, ...state } };
}

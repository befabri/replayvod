import type {
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { defineCaches, type EntityPatch, keyHasInput } from "@/lib/query";

// Every cache a video row lives in, plus the two statistics aggregates a
// download/watch mutation shifts (scalar: invalidated, never row-patched).
export function videoCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		listPage: { path: trpc.video.listPage, shape: "infinite" },
		byBroadcaster: { path: trpc.video.byBroadcaster, shape: "infinite" },
		byCategory: { path: trpc.video.byCategory, shape: "infinite" },
		search: { path: trpc.video.search, shape: "array" },
		getById: { path: trpc.video.getById, shape: "single" },
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
	"statistics",
	"statisticsByBroadcaster",
] as const;

// Merge the new user_state wherever the row appears, and drop it from filter-only
// lists it left (a watch-later list once un-flagged, an unwatched list once watched).
export function videoUserStatePatch(
	videoId: number,
	state: VideoUserStateResponse,
): EntityPatch<VideoResponse> {
	return {
		match: (video) => video.id === videoId,
		update: (video) => applyVideoUserState(video, state),
		removeFrom: (queryKey, shape) =>
			shape === "infinite" &&
			((!state.watch_later &&
				keyHasInput(queryKey, "watch_later_only", true)) ||
				(!!state.watched_at && keyHasInput(queryKey, "unwatched_only", true))),
	};
}

function applyVideoUserState(
	video: VideoResponse,
	state: VideoUserStateResponse,
): VideoResponse {
	return { ...video, user_state: { ...video.user_state, ...state } };
}

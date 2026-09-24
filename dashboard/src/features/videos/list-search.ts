import type { VideoSource, VideoStatus } from "@/api/generated/trpc";
import type { VideoOrder, VideoSort } from "./queries";

export type VideoListView = "grid" | "table";

export const VIDEO_LIST_VIEWS: readonly VideoListView[] = ["grid", "table"];

export type VideoListSortKey =
	| "recently_watched"
	| "newest"
	| "oldest"
	| "streamed_newest"
	| "streamed_oldest"
	| "channel_asc"
	| "channel_desc"
	| "longest"
	| "largest";

export const VIDEO_LIST_SORT_CONFIG: Record<
	VideoListSortKey,
	{ sort: VideoSort; order: VideoOrder }
> = {
	recently_watched: { sort: "last_watched", order: "desc" },
	newest: { sort: "created_at", order: "desc" },
	oldest: { sort: "created_at", order: "asc" },
	streamed_newest: { sort: "broadcast_at", order: "desc" },
	streamed_oldest: { sort: "broadcast_at", order: "asc" },
	channel_asc: { sort: "channel", order: "asc" },
	channel_desc: { sort: "channel", order: "desc" },
	longest: { sort: "duration", order: "desc" },
	largest: { sort: "size", order: "desc" },
};

export const VIDEO_LIST_SORT_KEYS = Object.keys(
	VIDEO_LIST_SORT_CONFIG,
) as VideoListSortKey[];

export const VIDEO_LIST_STATUSES = [
	"DONE",
	"RUNNING",
	"PENDING",
	"FAILED",
] as const satisfies readonly VideoStatus[];
export type VideoListStatus = (typeof VIDEO_LIST_STATUSES)[number];

export const VIDEO_LIST_TABS = [
	"all",
	"continue_watching",
	"this_week",
	"unwatched",
	"watch_later",
] as const;
export type VideoListTab = (typeof VIDEO_LIST_TABS)[number];

export const VIDEO_DURATION_FILTERS = [
	"short",
	"medium",
	"long",
	"marathon",
] as const;
export type VideoDurationFilter = (typeof VIDEO_DURATION_FILTERS)[number];

export const VIDEO_SOURCE_FILTERS = [
	"live",
	"vod",
] as const satisfies readonly VideoSource[];
export type VideoSourceFilter = (typeof VIDEO_SOURCE_FILTERS)[number];

export const VIDEO_QUALITY_LADDER = [
	"1080p60",
	"1080p",
	"720p60",
	"720p",
	"480p",
	"360p",
	"160p",
	"chunked",
	"audio_only",
] as const;

export const ANY_FILTER = "any";

export type VideoListFilters = {
	status?: VideoListStatus;
	quality?: string;
	language?: string;
	duration?: VideoDurationFilter;
	source?: VideoSourceFilter;
};

export function isOneOf<T extends string>(
	values: readonly T[],
	raw: unknown,
): raw is T {
	return typeof raw === "string" && values.includes(raw as T);
}

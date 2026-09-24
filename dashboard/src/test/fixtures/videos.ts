import type {
	ActiveDownloadResponse,
	TimelineEvent,
	VideoPartResponse,
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import type { TrpcHandlers } from "@/test/trpc-mock";
import { categoryAt } from "./categories";
import { channelAt } from "./channels";

export const VOD_TITLES: readonly string[] = [
	"Ranked grind until diamond",
	"Marathon caritatif, échauffement avec la commu",
	"Chill stream et jeux de société",
	"On teste les jeux les plus bizarres de Steam",
	"Speedrun any% avec les viewers",
	"Late night talk show, on répond à vos questions",
	"Minecraft hardcore jour 42",
	"Watch party des finales régionales",
	"Review des clips de la semaine",
];

export const VIDEO_QUALITIES = [
	"1080p60",
	"720p60",
	"480p",
	"audio_only",
] as const;

export const THUMBNAIL_COUNT = 6;

export const VIDEO_SNAPSHOTS: readonly string[] = [1, 2, 3, 4].map(
	(n) => `thumbnails/fixture-snap-${n}.svg`,
);

export const FIXTURE_NOW = Date.UTC(2026, 8, 21, 22, 0);

const HOUR_MS = 3600_000;

function stamp(date: Date): string {
	return date.toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
}

export function videoThumbnail(index: number): string {
	return `thumbnails/fixture-${(index % THUMBNAIL_COUNT) + 1}.svg`;
}

export function makeUserState(
	overrides: Partial<VideoUserStateResponse> = {},
): VideoUserStateResponse {
	return {
		watch_later: false,
		last_position_seconds: 0,
		updated_at: new Date(FIXTURE_NOW).toISOString(),
		...overrides,
	};
}

export function makeVideo(
	index = 0,
	overrides: Partial<VideoResponse> = {},
): VideoResponse {
	const id = index + 1;
	const channel = channelAt(index);
	const category = categoryAt(index * 3);
	const quality =
		overrides.quality ?? VIDEO_QUALITIES[(index * 3) % VIDEO_QUALITIES.length];
	const audioOnly = overrides.is_audio_only ?? quality === "audio_only";
	const durationSeconds = 1800 + ((index * 2797) % 27000);
	const startedAt = new Date(FIXTURE_NOW - index * 5.5 * HOUR_MS);
	return {
		id,
		job_id: `job-${id}`,
		filename: `${stamp(startedAt)}-${channel.login}-${id.toString(16).padStart(8, "0")}`,
		display_name: channel.displayName,
		title: VOD_TITLES[(index * 7) % VOD_TITLES.length],
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality,
		is_audio_only: audioOnly,
		broadcaster_id: channel.id,
		broadcaster_login: channel.login,
		broadcaster_name: channel.displayName,
		profile_image_url: channel.profileImageUrl,
		primary_category_id: category.id,
		primary_category_name: category.name,
		viewer_count: 120 + ((index * 431) % 9000),
		language: index % 3 === 0 ? "en" : "fr",
		duration_seconds: durationSeconds,
		size_bytes: durationSeconds * (audioOnly ? 20_000 : 750_000),
		thumbnail: videoThumbnail(index),
		start_download_at: startedAt.toISOString(),
		downloaded_at: new Date(
			startedAt.getTime() + durationSeconds * 1000,
		).toISOString(),
		source: "live",
		has_media: true,
		...overrides,
	};
}

export const VIDEO_STATES = {
	done: {},
	resumable: {
		duration_seconds: 5400,
		user_state: makeUserState({ last_position_seconds: 2000 }),
	},
	watchLater: { user_state: makeUserState({ watch_later: true }) },
	recording: {
		status: "RUNNING",
		downloaded_at: undefined,
		duration_seconds: 5400,
	},
	queued: {
		status: "PENDING",
		downloaded_at: undefined,
		duration_seconds: undefined,
		size_bytes: undefined,
	},
	failed: { status: "FAILED", error: "stream went offline" },
	cancelled: { status: "FAILED", completion_kind: "cancelled" },
	partial: { completion_kind: "partial" },
	truncated: { truncated: true },
	archive: {
		source: "vod",
		broadcast_at: new Date(FIXTURE_NOW - 12 * 24 * HOUR_MS).toISOString(),
	},
	audioOnly: { quality: "audio_only", is_audio_only: true },
	noThumbnail: { thumbnail: undefined },
} satisfies Record<string, Partial<VideoResponse>>;

export type VideoState = keyof typeof VIDEO_STATES;

export const VIDEO_REMOVAL_STATES = {
	removed: {
		deleted_at: new Date(FIXTURE_NOW - 2 * 24 * HOUR_MS).toISOString(),
		deletion_kind: "manual",
		has_media: false,
	},
	missing: {
		deleted_at: new Date(FIXTURE_NOW - 2 * 24 * HOUR_MS).toISOString(),
		deletion_kind: "missing",
		has_media: false,
	},
	pendingDeletion: {
		delete_requested_at: new Date(FIXTURE_NOW - HOUR_MS).toISOString(),
	},
} satisfies Record<string, Partial<VideoResponse>>;

export const VIDEO_STATE_NAMES = Object.keys(VIDEO_STATES) as VideoState[];

export function makeVideos(
	count: number,
	overrides?: (index: number) => Partial<VideoResponse>,
): VideoResponse[] {
	return Array.from({ length: count }, (_, index) =>
		makeVideo(index, overrides?.(index)),
	);
}

export function makeActiveDownload(
	overrides: Partial<ActiveDownloadResponse> = {},
): ActiveDownloadResponse {
	return {
		video: makeVideo(0, { status: "RUNNING" }),
		part_index: 1,
		stage: "segments",
		bytes_written: 100,
		segments_done: 10,
		segments_gaps: 0,
		segments_ad_gaps: 0,
		segments_total: -1,
		percent: -1,
		...overrides,
	};
}

export function makeVideoPart(
	overrides: Partial<VideoPartResponse> = {},
): VideoPartResponse {
	const partIndex = overrides.part_index ?? 1;
	return {
		id: partIndex,
		part_index: partIndex,
		filename: `vod-part${String(partIndex).padStart(2, "0")}.mp4`,
		quality: "1080p60",
		codec: "avc1",
		segment_format: "ts",
		duration_seconds: 10,
		size_bytes: 100,
		start_media_seq: 1,
		...overrides,
	};
}

export function makeTimelineEvent(
	overrides: Partial<TimelineEvent> = {},
): TimelineEvent {
	return { occurred_at: "2026-01-01T00:00:00Z", ...overrides };
}

export function makeTimeline(video: VideoResponse): TimelineEvent[] {
	const start = Date.parse(video.start_download_at);
	const category = categoryAt(video.id);
	return [
		{
			occurred_at: new Date(start).toISOString(),
			media_offset_seconds: 0,
			title: { id: video.id * 10, name: video.title },
			category: { id: category.id, name: category.name },
		},
		{
			occurred_at: new Date(start + 45 * 60_000).toISOString(),
			media_offset_seconds: 45 * 60,
			category: {
				id: categoryAt(video.id + 1).id,
				name: categoryAt(video.id + 1).name,
			},
		},
		{
			occurred_at: new Date(start + 95 * 60_000).toISOString(),
			media_offset_seconds: 95 * 60,
			title: {
				id: video.id * 10 + 1,
				name: VOD_TITLES[(video.id + 2) % VOD_TITLES.length],
			},
		},
	];
}

export function videoHandlers(videos: readonly VideoResponse[]) {
	const byId = new Map(videos.map((video) => [video.id, video]));
	return {
		video: {
			snapshots: () => [...VIDEO_SNAPSHOTS],
			timeline: ({ video_id }) => {
				const video = byId.get(video_id);
				return video ? makeTimeline(video) : [];
			},
			setWatchLater: ({ watch_later }) => makeUserState({ watch_later }),
			delete: () => ({ ok: true }),
			restore: () => ({ ok: true }),
		},
	} satisfies TrpcHandlers;
}

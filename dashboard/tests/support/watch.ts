import type { Page } from "@playwright/test";
import { mockTrpc, type TrpcResolver, trpcOk, validSession } from "./trpc";

export const recordedAt = "2026-06-05T12:00:00Z";

// audioOnlyVideo is a finished single-part audio recording the way
// video.getById returns it. `overrides` layers spec-specific fields on top,
// user_state for instance.
export function audioOnlyVideo(
	id: number,
	durationSeconds: number,
	overrides: Record<string, unknown> = {},
) {
	return {
		id,
		job_id: `job-${id}`,
		filename: "steam-nukes-indies",
		display_name: "ThornityCo",
		title: "Steam nukes indies. GL HF.",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "audio_only",
		codec: "aac",
		is_audio_only: true,
		broadcaster_id: "chan1",
		broadcaster_login: "thornityco",
		broadcaster_name: "ThornityCo",
		profile_image_url: "",
		primary_category_id: "software",
		primary_category_name: "Software and Game Development",
		viewer_count: 0,
		language: "en",
		duration_seconds: durationSeconds,
		size_bytes: 99_000_000,
		start_download_at: recordedAt,
		downloaded_at: recordedAt,
		parts: [
			{
				part_index: 1,
				filename: "steam-nukes-indies-part1.m4a",
				quality: "audio_only",
				codec: "aac",
				segment_format: "fmp4",
				duration_seconds: durationSeconds,
				size_bytes: 99_000_000,
				start_media_seq: 0,
				end_media_seq: 12,
			},
		],
		playback_artifact: {
			status: "unavailable",
			updated_at: recordedAt,
		},
		...overrides,
	};
}

// mockWatchPage answers every procedure the watch page asks for around one
// recording. `video` runs per request so mutable spec state (a saved
// position) stays live across reloads; `resolve` runs first so a spec can
// take over single procedures.
export async function mockWatchPage(
	page: Page,
	{
		video,
		resolve,
	}: { video: () => Record<string, unknown>; resolve?: TrpcResolver },
) {
	await mockTrpc(page, (procs, url) => {
		const custom = resolve?.(procs, url);
		if (custom) return custom;
		const session = validSession(procs, "");
		if (session) return session;
		const current = video();
		const duration = Number(current.duration_seconds ?? 0);
		const answers: Record<string, () => unknown> = {
			"video.getById": () => current,
			"video.timeline": () => [],
			"video.categories": () => [
				{
					id: "software",
					name: "Software and Game Development",
					started_at: recordedAt,
					duration_seconds: duration,
				},
			],
			"video.titles": () => [
				{
					id: 1,
					name: String(current.title ?? ""),
					started_at: recordedAt,
					duration_seconds: duration,
				},
			],
			"channel.getById": () => ({
				broadcaster_id: "chan1",
				broadcaster_login: "thornityco",
				broadcaster_name: "ThornityCo",
				profile_image_url: "",
				view_count: 0,
				created_at: recordedAt,
				updated_at: recordedAt,
			}),
			"video.statisticsByBroadcaster": () => ({
				total: 1,
				total_size: 99_000_000,
				total_duration_seconds: duration,
			}),
			"stream.latestLive": () => [],
		};
		if (!procs.some((proc) => proc in answers)) return null;
		return {
			status: 200,
			body: trpcOk(procs.map((proc) => answers[proc]?.() ?? null)),
		};
	});
}

// videoRecording is a finished single-part video recording the way
// video.getById returns it. The part streams from parts/1/stream; with no
// playback artifact the player sequences parts, so one mp4 is all it needs.
export function videoRecording(
	id: number,
	durationSeconds: number,
	overrides: Record<string, unknown> = {},
) {
	return {
		...audioOnlyVideo(id, durationSeconds),
		filename: "resume-fixture",
		title: "Resume fixture",
		quality: "1080p60",
		codec: "h264",
		is_audio_only: false,
		parts: [
			{
				part_index: 1,
				filename: "resume-fixture-part1.mp4",
				quality: "1080p60",
				codec: "h264",
				segment_format: "fmp4",
				duration_seconds: durationSeconds,
				size_bytes: 8807,
				start_media_seq: 0,
				end_media_seq: 6,
			},
		],
		...overrides,
	};
}

// userState is a video.getById user_state for a recording the user started.
export function userState(position: number) {
	return {
		watch_later: false,
		last_position_seconds: position,
		watched_at: position > 0 ? recordedAt : undefined,
		updated_at: recordedAt,
	};
}

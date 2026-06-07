import { describe, expect, it } from "vitest";

import type { VideoResponse } from "@/api/generated/trpc";
import {
	filterLoadedVideosForSearch,
	validateVideosSearch,
	videosSearchForTabChange,
} from "./videos";

describe("validateVideosSearch", () => {
	it("keeps supported library tabs", () => {
		expect(validateVideosSearch({ tab: "watch_later" }).tab).toBe(
			"watch_later",
		);
		expect(validateVideosSearch({ tab: "unwatched" }).tab).toBe("unwatched");
	});

	it("preserves the old favorites URL as watch later", () => {
		expect(validateVideosSearch({ tab: "favorites" }).tab).toBe("watch_later");
	});

	it("does not route the old partial download state as a library tab", () => {
		expect(validateVideosSearch({ tab: "partial" }).tab).toBe("all");
	});

	it("clears video filters when changing tabs", () => {
		const search = validateVideosSearch({
			tab: "all",
			status: "FAILED",
			view: "table",
			sort: "oldest",
			quality: "720p",
			language: "fr",
			duration: "long",
		});

		expect(videosSearchForTabChange(search, "watch_later")).toEqual({
			tab: "watch_later",
			status: undefined,
			view: "table",
			sort: "oldest",
			quality: undefined,
			language: undefined,
			duration: undefined,
		});
	});

	it("narrows placeholder rows by current tab and status", () => {
		const rows = [
			video({
				id: 1,
				status: "DONE",
				user_state: {
					watch_later: true,
					last_position_seconds: 0,
					updated_at: "2026-01-01T00:00:00Z",
				},
			}),
			video({
				id: 2,
				status: "DONE",
				user_state: {
					watch_later: false,
					last_position_seconds: 0,
					updated_at: "2026-01-01T00:00:00Z",
				},
			}),
			video({
				id: 3,
				status: "RUNNING",
				user_state: {
					watch_later: true,
					last_position_seconds: 0,
					updated_at: "2026-01-01T00:00:00Z",
				},
			}),
		];

		expect(
			filterLoadedVideosForSearch(rows, {
				tab: "watch_later",
				status: "DONE",
				quality: undefined,
				language: undefined,
				duration: undefined,
			}).map((row) => row.id),
		).toEqual([1]);
	});

	it("narrows placeholder rows for unwatched and this week tabs", () => {
		const nowMs = Date.parse("2026-06-07T12:00:00Z");
		const rows = [
			video({
				id: 1,
				status: "DONE",
				start_download_at: "2026-06-06T12:00:00Z",
			}),
			video({
				id: 2,
				status: "DONE",
				start_download_at: "2026-06-06T12:00:00Z",
				user_state: {
					watch_later: false,
					last_position_seconds: 10,
					watched_at: "2026-06-06T12:10:00Z",
					updated_at: "2026-06-06T12:10:00Z",
				},
			}),
			video({
				id: 3,
				status: "RUNNING",
				start_download_at: "2026-06-06T12:00:00Z",
			}),
			video({
				id: 4,
				status: "DONE",
				start_download_at: "2026-05-01T12:00:00Z",
			}),
		];

		expect(
			filterLoadedVideosForSearch(rows, {
				tab: "unwatched",
				status: undefined,
				quality: undefined,
				language: undefined,
				duration: undefined,
			}).map((row) => row.id),
		).toEqual([1, 4]);

		expect(
			filterLoadedVideosForSearch(
				rows,
				{
					tab: "this_week",
					status: undefined,
					quality: undefined,
					language: undefined,
					duration: undefined,
				},
				nowMs,
			).map((row) => row.id),
		).toEqual([1, 2, 3]);
	});
});

function video(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return {
		id: overrides.id ?? 1,
		job_id: overrides.job_id ?? `job-${overrides.id ?? 1}`,
		filename: overrides.filename ?? `video-${overrides.id ?? 1}.mp4`,
		display_name: overrides.display_name ?? "Channel",
		title: overrides.title ?? "Video",
		status: overrides.status ?? "DONE",
		completion_kind: overrides.completion_kind ?? "complete",
		truncated: overrides.truncated ?? false,
		quality: overrides.quality ?? "1080p",
		is_audio_only: overrides.is_audio_only ?? false,
		broadcaster_id: overrides.broadcaster_id ?? "bc-1",
		viewer_count: overrides.viewer_count ?? 0,
		language: overrides.language ?? "en",
		start_download_at: overrides.start_download_at ?? "2026-06-07T00:00:00Z",
		user_state: overrides.user_state,
	};
}

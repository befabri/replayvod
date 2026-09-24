import { describe, expect, it } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";
import { makeVideo } from "@/test/fixtures";
import { PLAYBACK_SETTINGS } from "@/test/playback-settings";
import {
	filterLoadedVideosForSearch as filterWithPolicy,
	validateVideosSearch,
	videosSearchForTabChange,
} from "./videos";

describe("validateVideosSearch", () => {
	it("defaults Continue Watching to recently watched and preserves explicit sorts", () => {
		expect(validateVideosSearch({ tab: "continue_watching" }).sort).toBe(
			"recently_watched",
		);
		expect(
			validateVideosSearch({ tab: "continue_watching", sort: "oldest" }).sort,
		).toBe("oldest");
		const search = validateVideosSearch({
			tab: "all",
			sort: "oldest",
			status: "FAILED",
		});
		const next = videosSearchForTabChange(search, "continue_watching");
		expect(next.sort).toBe("recently_watched");
		expect(next.status).toBeUndefined();
		expect(videosSearchForTabChange(next, "all").sort).toBe("newest");
	});

	it("keeps supported library tabs", () => {
		expect(validateVideosSearch({ tab: "watch_later" }).tab).toBe(
			"watch_later",
		);
		expect(validateVideosSearch({ tab: "unwatched" }).tab).toBe("unwatched");
		expect(validateVideosSearch({ tab: "continue_watching" }).tab).toBe(
			"continue_watching",
		);
	});

	it("keeps a known source filter and drops an unknown one", () => {
		expect(validateVideosSearch({ source: "vod" }).source).toBe("vod");
		expect(validateVideosSearch({ source: "clips" }).source).toBeUndefined();
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

	it("shows only resumable started recordings, including rewatches", () => {
		const state = {
			watch_later: false,
			last_position_seconds: 60,
			watched_at: "2026-01-01T00:00:00Z",
			updated_at: "2026-01-01T00:00:00Z",
		};
		const rows = [
			video({ id: 1 }),
			video({ id: 2, user_state: state }),
			video({ id: 3, user_state: { ...state, last_position_seconds: 4 } }),
			{ ...video({ id: 4, user_state: state }), duration_seconds: 61 },
			video({
				id: 5,
				user_state: { ...state, completed_at: state.watched_at },
			}),
			video({ id: 6, status: "RUNNING", user_state: state }),
			video({ id: 7, user_state: { ...state, watched_at: undefined } }),
		];
		expect(
			filterLoadedVideosForSearch(
				rows,
				validateVideosSearch({ tab: "continue_watching" }),
			).map((row) => row.id),
		).toEqual([2, 5]);
	});

	it("narrows placeholder rows by source", () => {
		const rows = [
			video({ id: 1 }),
			{ ...video({ id: 2 }), source: "vod" as const },
		];
		const search = {
			tab: "all" as const,
			status: undefined,
			quality: undefined,
			language: undefined,
			duration: undefined,
		};
		expect(
			filterLoadedVideosForSearch(rows, { ...search, source: "vod" }).map(
				(v) => v.id,
			),
		).toEqual([2]);
		expect(
			filterLoadedVideosForSearch(rows, { ...search, source: "live" }).map(
				(v) => v.id,
			),
		).toEqual([1]);
		expect(
			filterLoadedVideosForSearch(rows, { ...search, source: undefined }),
		).toHaveLength(2);
	});

	it("narrows placeholder rows to their known quality labels", () => {
		const rows = [
			video({ id: 1, quality: "1080p" }),
			video({ id: 2, quality: "720p60" }),
		];
		expect(
			filterLoadedVideosForSearch(
				rows,
				validateVideosSearch({ quality: "1080p" }),
			).map((row) => row.id),
		).toEqual([1]);
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
				source: undefined,
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
				source: "live",
			}),
			video({
				id: 2,
				status: "DONE",
				start_download_at: "2026-06-06T12:00:00Z",
				source: "live",
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
				source: "live",
			}),
			video({
				id: 4,
				status: "DONE",
				start_download_at: "2026-05-01T12:00:00Z",
				source: "live",
			}),
		];

		expect(
			filterLoadedVideosForSearch(rows, {
				tab: "unwatched",
				status: undefined,
				quality: undefined,
				language: undefined,
				duration: undefined,
				source: undefined,
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
					source: undefined,
				},
				nowMs,
			).map((row) => row.id),
		).toEqual([1, 2, 3]);
	});
});

function video(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return makeVideo((overrides.id ?? 1) - 1, {
		quality: "1080p",
		language: "en",
		duration_seconds: undefined,
		start_download_at: "2026-06-07T00:00:00Z",
		...overrides,
	});
}

function filterLoadedVideosForSearch(
	rows: Parameters<typeof filterWithPolicy>[0],
	search: Parameters<typeof filterWithPolicy>[1],
	nowMs?: number,
) {
	return filterWithPolicy(rows, search, PLAYBACK_SETTINGS, nowMs);
}

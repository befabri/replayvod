import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import { type CacheGroup, patchEntity } from "@/lib/query";
import { PLAYBACK_SETTINGS } from "@/test/playback-settings";
import { videoUserStatePatch } from "./cache";

describe("Continue Watching progress patches", () => {
	it("applies a bookmark without replacing higher-revision progress", () => {
		const current: VideoUserStateResponse = {
			watch_later: false,
			last_position_seconds: 40,
			progress_revision: 3,
			watched_at: "2026-09-01T00:00:00Z",
			completed_at: "2026-09-01T00:00:01Z",
			updated_at: "2026-09-01T00:00:02Z",
		};
		const video = {
			id: 1,
			status: "DONE",
			duration_seconds: 100,
			user_state: current,
		} as VideoResponse;
		const patch = videoUserStatePatch(
			1,
			{
				watch_later: true,
				last_position_seconds: 99,
				progress_revision: 2,
				updated_at: "2026-09-01T00:00:09Z",
			},
			PLAYBACK_SETTINGS,
		);
		const updated = patch.update(video);
		expect(updated.user_state).toEqual({ ...current, watch_later: true });
		expect(
			patch.removeFrom?.(
				[["video", "listPage"], { input: { continue_watching_only: true } }],
				"infinite",
				updated,
			),
		).toBe(false);
		expect(
			patch.removeFrom?.(
				[["video", "listPage"], { input: { unwatched_only: true } }],
				"infinite",
				updated,
			),
		).toBe(true);
	});

	it.each([
		{ duration: 100, position: 94, removed: false },
		{ duration: 100, position: 95, removed: true },
		{ duration: 1000, position: 969, removed: false },
		{ duration: 1000, position: 970, removed: true },
		{ duration: 0, position: 9000, removed: false },
		{ duration: 100, position: 4, removed: true },
	])("patches $duration seconds at $position before refetching", ({
		duration,
		position,
		removed,
	}) => {
		const qc = new QueryClient();
		const caches: CacheGroup = {
			library: { pathKey: [["video", "listPage"]], shape: "infinite" },
			preview: { pathKey: [["video", "continueWatching"]], shape: "array" },
			search: { pathKey: [["video", "search"]], shape: "array" },
			one: { pathKey: [["video", "getById"]], shape: "single" },
		};
		const state: VideoUserStateResponse = {
			watch_later: false,
			last_position_seconds: 40,
			watched_at: "2026-09-01T00:00:00Z",
			updated_at: "2026-09-01T00:00:00Z",
			// Completing a past viewing must not prevent a later rewatch.
			completed_at: "2026-09-01T00:00:00Z",
		};
		const video = {
			id: 1,
			status: "DONE",
			duration_seconds: duration,
			user_state: state,
		} as VideoResponse;
		const continueKey = [
			...caches.library.pathKey,
			{ input: { continue_watching_only: true } },
		];
		const allKey = [
			...caches.library.pathKey,
			{ input: { continue_watching_only: false } },
		];
		for (const key of [continueKey, allKey]) {
			qc.setQueryData(key, {
				pages: [{ items: [video], next_cursor: "next" }],
				pageParams: [null],
			});
		}
		qc.setQueryData(caches.preview.pathKey, [video]);
		qc.setQueryData(caches.search.pathKey, [video]);
		qc.setQueryData(caches.one.pathKey, video);
		const updated = { ...state, last_position_seconds: position };
		patchEntity(qc, caches, videoUserStatePatch(1, updated, PLAYBACK_SETTINGS));
		const patched = { ...video, user_state: updated };
		expect(qc.getQueryData(caches.preview.pathKey)).toEqual(
			removed ? [] : [patched],
		);
		expect(qc.getQueryData(continueKey)).toEqual({
			pages: [{ items: removed ? [] : [patched], next_cursor: "next" }],
			pageParams: [null],
		});
		expect(qc.getQueryData(allKey)).toEqual({
			pages: [{ items: [patched], next_cursor: "next" }],
			pageParams: [null],
		});
		expect(qc.getQueryData(caches.search.pathKey)).toEqual([patched]);
		expect(qc.getQueryData(caches.one.pathKey)).toEqual(patched);
	});
});

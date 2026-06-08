import { type InfiniteData, QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
	VideoListPageResponse,
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { patchEntity } from "@/lib/query";
import { videoCaches, videoUserStatePatch } from "./cache";

function video(partial: Partial<VideoResponse>): VideoResponse {
	return {
		id: partial.id ?? 1,
		job_id: partial.job_id ?? `job-${partial.id ?? 1}`,
		filename: partial.filename ?? `video-${partial.id ?? 1}.mp4`,
		display_name: partial.display_name ?? "Channel",
		title: partial.title ?? "Video",
		status: partial.status ?? "DONE",
		completion_kind: partial.completion_kind ?? "complete",
		truncated: partial.truncated ?? false,
		quality: partial.quality ?? "1080p",
		is_audio_only: partial.is_audio_only ?? false,
		broadcaster_id: partial.broadcaster_id ?? "bc-1",
		viewer_count: partial.viewer_count ?? 0,
		language: partial.language ?? "en",
		start_download_at: partial.start_download_at ?? "2026-01-01T00:00:00Z",
		user_state: partial.user_state,
	};
}

function pages(items: VideoResponse[]): InfiniteData<VideoListPageResponse> {
	return { pages: [{ items }], pageParams: [undefined] };
}

function fakeTrpc(): ReturnType<typeof useTRPC> {
	const node = (name: string) => ({ pathKey: () => [["video", name]] });
	return {
		video: {
			listPage: node("listPage"),
			byBroadcaster: node("byBroadcaster"),
			byCategory: node("byCategory"),
			search: node("search"),
			getById: node("getById"),
			statistics: node("statistics"),
			statisticsByBroadcaster: node("statisticsByBroadcaster"),
		},
	} as unknown as ReturnType<typeof useTRPC>;
}

const clearedWatchLater: VideoUserStateResponse = {
	watch_later: false,
	last_position_seconds: 0,
	updated_at: "2026-01-02T00:00:00Z",
};

function listPageKey(input: Record<string, unknown>) {
	return [["video", "listPage"], { input, type: "infinite" }];
}

describe("videoUserStatePatch via patchEntity", () => {
	it("updates a video in a normal list without removing it", () => {
		const qc = new QueryClient();
		const caches = videoCaches(fakeTrpc());
		qc.setQueryData(
			listPageKey({ scope: "" }),
			pages([
				video({
					id: 1,
					user_state: { ...clearedWatchLater, watch_later: true },
				}),
			]),
		);

		patchEntity(qc, caches, videoUserStatePatch(1, clearedWatchLater));

		const next = qc.getQueryData<InfiniteData<VideoListPageResponse>>(
			listPageKey({ scope: "" }),
		);
		expect(next?.pages[0]?.items).toHaveLength(1);
		expect(next?.pages[0]?.items[0]?.user_state?.watch_later).toBe(false);
	});

	it("removes an un-flagged video from a watch-later-only list", () => {
		const qc = new QueryClient();
		const caches = videoCaches(fakeTrpc());
		qc.setQueryData(
			listPageKey({ watch_later_only: true }),
			pages([
				video({
					id: 1,
					user_state: { ...clearedWatchLater, watch_later: true },
				}),
				video({
					id: 2,
					user_state: { ...clearedWatchLater, watch_later: true },
				}),
			]),
		);

		patchEntity(qc, caches, videoUserStatePatch(1, clearedWatchLater));

		const next = qc.getQueryData<InfiniteData<VideoListPageResponse>>(
			listPageKey({ watch_later_only: true }),
		);
		expect(next?.pages[0]?.items.map((item) => item.id)).toEqual([2]);
	});

	it("removes a watched video from an unwatched-only list", () => {
		const qc = new QueryClient();
		const caches = videoCaches(fakeTrpc());
		qc.setQueryData(
			listPageKey({ unwatched_only: true }),
			pages([video({ id: 1 }), video({ id: 2 })]),
		);

		patchEntity(
			qc,
			caches,
			videoUserStatePatch(1, {
				watch_later: false,
				last_position_seconds: 45,
				watched_at: "2026-01-02T00:00:00Z",
				updated_at: "2026-01-02T00:00:00Z",
			}),
		);

		const next = qc.getQueryData<InfiniteData<VideoListPageResponse>>(
			listPageKey({ unwatched_only: true }),
		);
		expect(next?.pages[0]?.items.map((item) => item.id)).toEqual([2]);
	});

	it("updates the single video cache and merges user_state", () => {
		const qc = new QueryClient();
		const caches = videoCaches(fakeTrpc());
		const getByIdKey = [["video", "getById"], { input: { id: 1 } }];
		qc.setQueryData(getByIdKey, video({ id: 1 }));

		patchEntity(
			qc,
			caches,
			videoUserStatePatch(1, {
				watch_later: false,
				last_position_seconds: 45,
				watched_at: "2026-01-02T00:00:00Z",
				updated_at: "2026-01-02T00:00:00Z",
			}),
		);

		const next = qc.getQueryData<VideoResponse>(getByIdKey);
		expect(next?.user_state?.last_position_seconds).toBe(45);
		expect(next?.user_state?.watched_at).toBe("2026-01-02T00:00:00Z");
	});
});

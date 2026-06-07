import { type InfiniteData, QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
	VideoListPageResponse,
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import {
	applyVideoUserStateToVideoCaches,
	applyWatchLaterStateToInfiniteVideoPages,
	queryKeyHasUnwatchedOnly,
	queryKeyHasWatchLaterOnly,
} from "./queries";

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

function cache(items: VideoResponse[]): InfiniteData<VideoListPageResponse> {
	return {
		pages: [{ items }],
		pageParams: [undefined],
	};
}

const removedState: VideoUserStateResponse = {
	watch_later: false,
	last_position_seconds: 0,
	updated_at: "2026-01-02T00:00:00Z",
};

function fakeTrpcKeys(): Parameters<
	typeof applyVideoUserStateToVideoCaches
>[1] {
	const key = (name: string) => ({
		pathKey: () => [["video", name]],
	});
	return {
		video: {
			listPage: key("listPage"),
			byBroadcaster: key("byBroadcaster"),
			byCategory: key("byCategory"),
			search: key("search"),
			getById: key("getById"),
		},
	} as Parameters<typeof applyVideoUserStateToVideoCaches>[1];
}

describe("watch later query cache helpers", () => {
	it("updates normal cached lists without removing the video", () => {
		const next = applyWatchLaterStateToInfiniteVideoPages(
			cache([
				video({ id: 1, user_state: { ...removedState, watch_later: true } }),
			]),
			1,
			removedState,
		);

		expect(next?.pages[0]?.items).toHaveLength(1);
		expect(next?.pages[0]?.items[0]?.user_state?.watch_later).toBe(false);
	});

	it("removes the video from watch-later-only cached lists", () => {
		const next = applyWatchLaterStateToInfiniteVideoPages(
			cache([
				video({ id: 1, user_state: { ...removedState, watch_later: true } }),
				video({ id: 2, user_state: { ...removedState, watch_later: true } }),
			]),
			1,
			removedState,
			{ removeWhenNotWatchLater: true },
		);

		expect(next?.pages[0]?.items.map((item) => item.id)).toEqual([2]);
	});

	it("detects watch-later-only tRPC query keys", () => {
		expect(
			queryKeyHasWatchLaterOnly([
				["video", "listPage"],
				{ input: { watch_later_only: true }, type: "infinite" },
			]),
		).toBe(true);
		expect(
			queryKeyHasWatchLaterOnly([
				["video", "listPage"],
				{ input: { watch_later_only: false }, type: "infinite" },
			]),
		).toBe(false);
	});

	it("detects unwatched-only tRPC query keys", () => {
		expect(
			queryKeyHasUnwatchedOnly([
				["video", "listPage"],
				{ input: { unwatched_only: true }, type: "infinite" },
			]),
		).toBe(true);
		expect(
			queryKeyHasUnwatchedOnly([
				["video", "listPage"],
				{ input: { unwatched_only: false }, type: "infinite" },
			]),
		).toBe(false);
	});

	it("updates active tRPC infinite caches matched by endpoint path", () => {
		const queryClient = new QueryClient();
		const queryKey = [
			["video", "listPage"],
			{ input: { watch_later_only: true }, type: "infinite" },
		];
		queryClient.setQueryData(
			queryKey,
			cache([
				video({ id: 1, user_state: { ...removedState, watch_later: true } }),
				video({ id: 2, user_state: { ...removedState, watch_later: true } }),
			]),
		);

		queryClient.setQueriesData<InfiniteData<VideoListPageResponse>>(
			{
				queryKey: [["video", "listPage"]],
				predicate: (query) => queryKeyHasWatchLaterOnly(query.queryKey),
			},
			(data) =>
				applyWatchLaterStateToInfiniteVideoPages(data, 1, removedState, {
					removeWhenNotWatchLater: true,
				}),
		);

		const next =
			queryClient.getQueryData<InfiniteData<VideoListPageResponse>>(queryKey);
		expect(next?.pages[0]?.items.map((item) => item.id)).toEqual([2]);
	});

	it("applies progress state to the active video cache and unwatched tab", () => {
		const queryClient = new QueryClient();
		const getByIdKey = [["video", "getById"], { input: { id: 1 } }];
		const unwatchedKey = [
			["video", "listPage"],
			{ input: { unwatched_only: true }, type: "infinite" },
		];
		queryClient.setQueryData(getByIdKey, video({ id: 1 }));
		queryClient.setQueryData(
			unwatchedKey,
			cache([video({ id: 1 }), video({ id: 2 })]),
		);

		applyVideoUserStateToVideoCaches(queryClient, fakeTrpcKeys(), 1, {
			watch_later: false,
			last_position_seconds: 45,
			watched_at: "2026-01-02T00:00:00Z",
			updated_at: "2026-01-02T00:00:00Z",
		});

		const cachedVideo = queryClient.getQueryData<VideoResponse>(getByIdKey);
		expect(cachedVideo?.user_state?.last_position_seconds).toBe(45);
		const unwatched =
			queryClient.getQueryData<InfiniteData<VideoListPageResponse>>(
				unwatchedKey,
			);
		expect(unwatched?.pages[0]?.items.map((item) => item.id)).toEqual([2]);
	});
});

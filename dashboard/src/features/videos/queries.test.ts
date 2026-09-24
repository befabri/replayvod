// @vitest-environment jsdom

import {
	type InfiniteData,
	QueryClient,
	QueryClientProvider,
} from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
	RelatedRecordingsResponse,
	VideoListPageResponse,
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider, useTRPC } from "@/api/trpc";
import { patchEntity } from "@/lib/query";
import { makeVideo, VIDEO_STATES } from "@/test/fixtures";
import { USER_SETTINGS } from "@/test/playback-settings";
import { videoCaches, videoUserStatePatch } from "./cache";
import {
	prefetchVideo,
	useCachedVideo,
	useCancelDownload,
	useDeleteVideo,
	useLiveVideoChanges,
	useRelatedRecordings,
	useRestoreVideo,
	useSetWatchLater,
	useVideo,
} from "./queries";

const subscription = vi.hoisted(() => ({
	onData: undefined as (() => Promise<unknown>) | undefined,
	onStarted: undefined as (() => Promise<unknown>) | undefined,
}));
vi.mock("@trpc/tanstack-react-query", async (original) => ({
	...(await original<typeof import("@trpc/tanstack-react-query")>()),
	useSubscription: (options: typeof subscription) =>
		Object.assign(subscription, options),
}));

afterEach(() => {
	cleanup();
	vi.useRealTimers();
});

function video(partial: Partial<VideoResponse>): VideoResponse {
	return makeVideo((partial.id ?? 1) - 1, {
		quality: "1080p",
		language: "en",
		duration_seconds: undefined,
		start_download_at: "2026-01-01T00:00:00Z",
		...partial,
	});
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
			continueWatching: node("continueWatching"),
			getById: node("getById"),
			relatedRecordings: node("relatedRecordings"),
			historyCounts: node("historyCounts"),
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

function hookHarness(
	respond: (procedure: string, input: { id?: number }) => unknown,
) {
	vi.useFakeTimers();
	const calls: string[] = [];
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
		},
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (input, init) => {
					const url = new URL(String(input));
					const procedure = url.pathname.replace("/trpc/", "");
					calls.push(procedure);
					const args = JSON.parse(
						init?.method === "POST"
							? String(init.body)
							: (url.searchParams.get("input") ?? "{}"),
					);
					const data = await respond(procedure, args);
					return new Response(JSON.stringify({ result: { data } }), {
						headers: { "Content-Type": "application/json" },
					});
				},
			}),
		],
	});
	const wrapper = ({ children }: { children: ReactNode }) =>
		createElement(
			QueryClientProvider,
			{ client: queryClient },
			createElement(TRPCProvider, { trpcClient, queryClient, children }),
		);
	return { wrapper, calls, queryClient };
}

async function advance(ms = 1) {
	await act(async () => {
		await vi.advanceTimersByTimeAsync(ms);
	});
}

describe("watch-later response ordering", () => {
	it("keeps newer resumable progress when a bookmark response arrives late", async () => {
		let reply!: (state: VideoUserStateResponse) => void;
		const pending = new Promise<VideoUserStateResponse>((resolve) => {
			reply = resolve;
		});
		const { wrapper, calls, queryClient } = hookHarness(() => pending);
		const { result } = renderHook(
			() => ({ mutation: useSetWatchLater(), trpc: useTRPC() }),
			{ wrapper },
		);
		const trpc = result.current.trpc;
		const detailKey = trpc.video.getById.queryKey({ id: 1 });
		const continueKey = listPageKey({ continue_watching_only: true });
		const previewKey = trpc.video.continueWatching.queryKey({ limit: 5 });
		queryClient.setQueryData(trpc.settings.get.queryKey(), USER_SETTINGS);
		const oldState: VideoUserStateResponse = {
			watch_later: false,
			last_position_seconds: 4,
			progress_revision: 2,
			watched_at: "2026-01-01T00:00:00Z",
			updated_at: "2026-01-01T00:00:00Z",
		};
		queryClient.setQueryData(detailKey, video({ user_state: oldState }));
		let mutation!: Promise<VideoUserStateResponse>;
		act(() => {
			mutation = result.current.mutation.mutateAsync({
				video_id: 1,
				watch_later: true,
			});
		});
		await advance();
		expect(calls).toEqual(["video.setWatchLater"]);
		const freshState: VideoUserStateResponse = {
			...oldState,
			watch_later: true,
			last_position_seconds: 900,
			progress_revision: 3,
			completed_at: "2026-01-01T00:00:01Z",
			updated_at: "2026-01-01T00:00:02Z",
		};
		const freshVideo = video({
			duration_seconds: 3600,
			user_state: freshState,
		});
		queryClient.setQueryData(detailKey, freshVideo);
		queryClient.setQueryData(continueKey, pages([freshVideo]));
		queryClient.setQueryData(previewKey, [freshVideo]);
		await act(async () => {
			reply({ ...oldState, watch_later: true });
			await mutation;
		});
		expect(
			queryClient.getQueryData<VideoResponse>(detailKey)?.user_state,
		).toEqual(freshState);
		expect(queryClient.getQueryData(continueKey)).toEqual(pages([freshVideo]));
		expect(queryClient.getQueryData(previewKey)).toEqual([freshVideo]);
		expect(queryClient.getQueryState(continueKey)?.isInvalidated).toBe(true);
		expect(calls).toEqual(["video.setWatchLater"]);
	});
});

function related(
	status: RelatedRecordingsResponse["status"] = "expired",
): RelatedRecordingsResponse {
	return {
		intent_id: "manual",
		status,
		items: [1, 2].map((id) => ({
			id,
			job_id: `job-${id}`,
			position: id,
			title: `Recording ${id}`,
			status: "DONE",
			completion_kind: "complete",
			started_at: "2026-01-01T00:00:00Z",
		})),
	};
}

describe("related recording cache lifecycle", () => {
	it.each([
		"delete",
		"restore",
		"cancel",
	] as const)("%s refreshes related windows even after polling has stopped", async (action) => {
		let response = related();
		const { wrapper } = hookHarness((procedure) => {
			if (procedure === "video.relatedRecordings") return response;
			response = {
				...response,
				items: response.items.map((item) => ({ ...item, title: "Updated" })),
			};
			return { ok: true };
		});
		const { result } = renderHook(
			() => ({
				first: useRelatedRecordings(1),
				second: useRelatedRecordings(2),
				remove: useDeleteVideo(),
				restore: useRestoreVideo(),
				cancel: useCancelDownload(),
			}),
			{ wrapper },
		);
		await advance();
		expect(result.current.first.data?.items[0].title).toBe("Recording 1");
		await act(async () => {
			if (action === "delete")
				await result.current.remove.mutateAsync({ id: 1 });
			else if (action === "restore")
				await result.current.restore.mutateAsync({ id: 1 });
			else await result.current.cancel.mutateAsync({ job_id: "job-1" });
		});
		await advance();
		expect(result.current.first.data?.items[0].title).toBe("Updated");
		expect(result.current.second.data?.items[0].title).toBe("Updated");
	});

	it("keeps navigation for a known sibling without reusing the old video or leaking an unrelated window", async () => {
		const { wrapper } = hookHarness((procedure, { id }) => {
			if (id !== 1) return new Promise(() => {});
			return procedure === "video.relatedRecordings"
				? related()
				: video({ id });
		});
		const { result, rerender } = renderHook(
			({ id }) => ({
				related: useRelatedRecordings(id),
				video: useVideo(id),
			}),
			{ wrapper, initialProps: { id: 1 } },
		);
		await advance();
		expect(result.current.video.data?.id).toBe(1);
		rerender({ id: 2 });
		expect(result.current.related.data?.items).toHaveLength(2);
		expect(result.current.related.isPlaceholderData).toBe(true);
		expect(result.current.video.data).toBeUndefined();
		rerender({ id: 999 });
		expect(result.current.related.data).toBeUndefined();
	});
});

describe("watch page preloading", () => {
	it("hands the watch page a recording the library already loaded", () => {
		const { wrapper, queryClient } = hookHarness(() => new Promise(() => {}));
		const listed = video({ id: 7, thumbnail: "thumbnails/listed.jpg" });
		queryClient.setQueryData(
			listPageKey({ limit: 50 }),
			pages([video({ id: 6 }), listed]),
		);
		queryClient.setQueryData(
			[["video", "relatedRecordings"], { input: { id: 1 }, type: "query" }],
			related(),
		);

		const { result, rerender } = renderHook(({ id }) => useCachedVideo(id), {
			wrapper,
			initialProps: { id: 7 },
		});
		expect(result.current).toBe(listed);

		// Related-recording items carry an id and a job_id too, but none of the
		// fields the poster needs, so they must never pass for a recording.
		rerender({ id: 2 });
		expect(result.current).toBeUndefined();
	});

	// The skeleton stays on screen while settings or the detail load. Whatever
	// reaches the cache in the meantime must reach it too, or it keeps a plain
	// box, or the wrong player shape, until the page replaces it.
	it("picks up a recording that reaches the cache after the loading state mounts", async () => {
		const { wrapper, queryClient } = hookHarness(() => new Promise(() => {}));
		const { result } = renderHook(() => useCachedVideo(7), { wrapper });
		expect(result.current).toBeUndefined();

		const listed = video({ id: 7, ...VIDEO_STATES.audioOnly });
		queryClient.setQueryData(listPageKey({ limit: 50 }), pages([listed]));
		await advance();

		expect(result.current).toBe(listed);
	});

	// Moving between related recordings must never wait on the destination's
	// playback data, so the loader only starts the request.
	it("starts loading the recording without holding navigation", async () => {
		let reply!: (value: VideoResponse) => void;
		const { wrapper, calls, queryClient } = hookHarness(
			() => new Promise<VideoResponse>((resolve) => (reply = resolve)),
		);
		const { result: trpc } = renderHook(() => useTRPC(), { wrapper });

		expect(prefetchVideo(queryClient, trpc.current, 5)).toBeUndefined();
		await advance();
		expect(calls).toEqual(["video.getById"]);

		reply(video({ id: 5, ...VIDEO_STATES.audioOnly }));
		await advance();
		const { result } = renderHook(() => useVideo(5), { wrapper });
		expect(result.current.data?.is_audio_only).toBe(true);
		expect(calls).toEqual(["video.getById"]);
	});

	it("skips ids that cannot exist", async () => {
		const { wrapper, calls, queryClient } = hookHarness((_, { id }) =>
			video({ id }),
		);
		const { result: trpc } = renderHook(() => useTRPC(), { wrapper });

		for (const id of [Number.NaN, 0, -3, 1.5]) {
			prefetchVideo(queryClient, trpc.current, id);
		}
		await advance();

		expect(calls).toEqual([]);
	});

	// Hovering a card preloads the watch page. It must not ask again for a
	// recording it already has, and a failing id must not keep retrying in
	// the background for a page the user may never open.
	it("keeps hover preloads to one request per recording", async () => {
		const { wrapper, calls, queryClient } = hookHarness((_, { id }) => {
			if (id === 9) throw new Error("gone");
			return video({ id });
		});
		queryClient.setQueryDefaults([["video", "getById"]], {
			retry: 3,
			staleTime: 0,
		});
		const { result: trpc } = renderHook(() => useTRPC(), { wrapper });

		prefetchVideo(queryClient, trpc.current, 5, { preload: true });
		await advance();
		prefetchVideo(queryClient, trpc.current, 5, { preload: true });
		prefetchVideo(queryClient, trpc.current, 9, { preload: true });
		await advance(60_000);

		expect(calls).toEqual(["video.getById", "video.getById"]);
	});
});

describe("recording notifications", () => {
	it.each([
		"active",
		"waiting",
		"stopped",
		"expired",
		undefined,
	] as const)("never polls an intent with status %s", async (status) => {
		const response = related();
		response.status = status;
		response.wait_until = new Date(Date.now() + 120_000).toISOString();
		const { wrapper, calls } = hookHarness(() => response);
		renderHook(() => useRelatedRecordings(1), { wrapper });
		await advance(180_000);
		expect(calls).toHaveLength(1);
	});

	it.each([
		"PENDING",
		"RUNNING",
	] as const)("refreshes a %s member on notification, including after intent closure", async (status) => {
		const response = related("stopped");
		response.items[1].status = status;
		const { wrapper, calls } = hookHarness((procedure) =>
			procedure === "video.relatedRecordings"
				? response
				: video({ id: 2, status: response.items[1].status }),
		);
		const { result } = renderHook(
			() => {
				useLiveVideoChanges();
				return { related: useRelatedRecordings(1), video: useVideo(2) };
			},
			{ wrapper },
		);
		await advance(120_000);
		expect(calls).toHaveLength(2);
		expect(result.current.video.data?.status).toBe(status);
		response.items[1].status = "DONE";
		await act(async () => {
			await subscription.onData?.();
		});
		await advance();
		expect(result.current.video.data?.status).toBe("DONE");
		expect(result.current.related.data?.items[1].status).toBe("DONE");
		expect(calls).toHaveLength(4);
	});

	it.each([
		"active",
		"waiting",
		"stopped",
		"expired",
	] as const)("recovers intent transition to %s on reconnect", async (status) => {
		const response = related("waiting");
		const { wrapper, queryClient } = hookHarness(() => response);
		const { result } = renderHook(
			() => {
				useLiveVideoChanges();
				return { query: useRelatedRecordings(1), trpc: useTRPC() };
			},
			{ wrapper },
		);
		await advance();
		const inactiveKey = result.current.trpc.video.relatedRecordings.queryKey({
			id: 2,
		});
		queryClient.setQueryData(inactiveKey, related("waiting"));
		response.status = status;
		await act(async () => {
			await subscription.onStarted?.();
		});
		await advance();
		expect(result.current.query.data?.status).toBe(status);
		expect(queryClient.getQueryState(inactiveKey)?.isInvalidated).toBe(true);
	});
});

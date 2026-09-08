// @vitest-environment jsdom

import {
	QueryClient,
	QueryClientProvider,
	type QueryKey,
} from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { ChannelVODsResponse } from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider, useTRPC } from "@/api/trpc";
import {
	CHANNEL_VODS_PAGE_SIZE,
	useChannelVods,
	useDequeueArchive,
	useEnqueueArchive,
} from "./queries";

afterEach(cleanup);

type Call = { procedure: string; method: string; input: unknown };

// harness wires the hooks to a fake tRPC transport; respond decides what each
// procedure returns and every request is recorded for assertions.
function harness(
	respond: (call: Call) => unknown,
	options: { staleTime?: number } = {},
) {
	const calls: Call[] = [];
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				retry: false,
				staleTime: options.staleTime ?? Number.POSITIVE_INFINITY,
				gcTime: Number.POSITIVE_INFINITY,
			},
			mutations: { retry: false },
		},
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (url, init) => {
					const parsed = new URL(String(url));
					const method = init?.method ?? "GET";
					const call: Call = {
						procedure: parsed.pathname.replace("/trpc/", ""),
						method,
						input:
							method === "POST"
								? JSON.parse(String(init?.body))
								: JSON.parse(parsed.searchParams.get("input") ?? "null"),
					};
					calls.push(call);
					return new Response(
						JSON.stringify({ result: { data: respond(call) } }),
						{
							headers: { "Content-Type": "application/json" },
						},
					);
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
	return { wrapper, queryClient, calls };
}

function vodPage(ids: string[], next_cursor?: string): ChannelVODsResponse {
	return {
		channel: { broadcaster_id: "u1", login: "streamer", name: "Streamer" },
		vods: ids.map((id) => ({
			id,
			title: `vod ${id}`,
			url: `https://www.twitch.tv/videos/${id}`,
			type: "archive",
			created_at: "2026-08-20T12:00:00Z",
			duration_seconds: 60,
			view_count: 1,
		})),
		next_cursor,
	};
}

describe("archive mutations refresh what they change", () => {
	for (const action of ["enqueue", "dequeue"] as const) {
		it(`${action} invalidates the queue, the channel browser and the video caches`, async () => {
			const { wrapper, queryClient, calls } = harness((call) =>
				call.procedure === "archive.enqueue"
					? { items: [{ input: "1", vod_id: "1", status: "queued" }] }
					: { ok: true },
			);
			const { result } = renderHook(
				() => ({
					trpc: useTRPC(),
					enqueue: useEnqueueArchive(),
					dequeue: useDequeueArchive(),
				}),
				{ wrapper },
			);
			const trpc = result.current.trpc;
			const touched: QueryKey[] = [
				trpc.archive.queue.queryKey(),
				trpc.archive.listChannelVods.infiniteQueryKey({
					channel: "streamer",
					limit: CHANNEL_VODS_PAGE_SIZE,
				}),
				trpc.video.listPage.infiniteQueryKey({ limit: 20 }),
				trpc.video.getById.queryKey({ id: 5 }),
				trpc.video.statistics.queryKey(),
			];
			const untouched: QueryKey[] = [trpc.task.list.queryKey()];
			for (const key of [...touched, ...untouched]) {
				queryClient.setQueryData(key, { seeded: true });
			}

			act(() => {
				if (action === "enqueue")
					result.current.enqueue.mutate({ vods: ["1"] });
				else result.current.dequeue.mutate({ video_id: 5 });
			});
			await waitFor(() =>
				expect(result.current[action].status).toBe("success"),
			);

			for (const key of touched) {
				expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
			}
			for (const key of untouched) {
				expect(queryClient.getQueryState(key)?.isInvalidated).toBe(false);
			}
			expect(calls).toEqual([
				{
					procedure: `archive.${action}`,
					method: "POST",
					input: action === "enqueue" ? { vods: ["1"] } : { video_id: 5 },
				},
			]);
		});
	}

	it("leaves the caches alone when the mutation fails", async () => {
		const { wrapper, queryClient } = harness(() => {
			throw new Error("enqueue rejected");
		});
		const { result } = renderHook(
			() => ({ trpc: useTRPC(), enqueue: useEnqueueArchive() }),
			{ wrapper },
		);
		const key = result.current.trpc.archive.queue.queryKey();
		queryClient.setQueryData(key, { queue: [], failures: [] });
		act(() => result.current.enqueue.mutate({ vods: ["1"] }));
		await waitFor(() => expect(result.current.enqueue.status).toBe("error"));
		expect(queryClient.getQueryState(key)?.isInvalidated).toBe(false);
	});
});

describe("useChannelVods", () => {
	it.each([null, ""])("does not look up channel %j", async (channel) => {
		const { wrapper, calls } = harness(() => vodPage(["1"]));
		const { result } = renderHook(() => useChannelVods(channel), { wrapper });
		await act(async () => {
			await new Promise((resolve) => setTimeout(resolve, 20));
		});
		expect(result.current.fetchStatus).toBe("idle");
		expect(result.current.data).toBeUndefined();
		expect(calls).toEqual([]);
	});

	it("pages with the server cursor until the last page", async () => {
		const { wrapper, calls } = harness((call) => {
			const input = call.input as { cursor?: string };
			return input.cursor === "c2" ? vodPage(["3"]) : vodPage(["1", "2"], "c2");
		});
		const { result } = renderHook(() => useChannelVods("streamer"), {
			wrapper,
		});
		await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
		expect(result.current.data?.pages[0].vods.map((v) => v.id)).toEqual([
			"1",
			"2",
		]);
		expect(result.current.hasNextPage).toBe(true);

		await act(async () => {
			await result.current.fetchNextPage();
		});
		await waitFor(() => expect(result.current.data?.pages).toHaveLength(2));
		expect(result.current.data?.pages[1].vods.map((v) => v.id)).toEqual(["3"]);
		expect(result.current.hasNextPage).toBe(false);

		expect(calls.map((c) => c.procedure)).toEqual([
			"archive.listChannelVods",
			"archive.listChannelVods",
		]);
		expect(calls[0].input).toMatchObject({
			channel: "streamer",
			limit: CHANNEL_VODS_PAGE_SIZE,
		});
		expect(calls[0].input).not.toHaveProperty("cursor");
		expect(calls[1].input).toMatchObject({
			channel: "streamer",
			limit: CHANNEL_VODS_PAGE_SIZE,
			cursor: "c2",
		});
	});

	it("does not retry a failed lookup on its own", async () => {
		let attempts = 0;
		const { wrapper } = harness(() => {
			attempts++;
			throw new Error("channel not found");
		});
		const { result } = renderHook(() => useChannelVods("nobody"), {
			wrapper,
		});
		await waitFor(() => expect(result.current.status).toBe("error"));
		expect(attempts).toBe(1);
	});
});

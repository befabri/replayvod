// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { afterEach, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import {
	useArchiveQueue,
	useLiveArchiveQueue,
} from "@/features/archive/queries";
import {
	useEventLogs,
	useLiveSystemEvents,
} from "@/features/eventlogs/queries";
import { useLiveTaskStatus, useTasks } from "@/features/tasks/queries";
import { useLiveVideoChanges, useVideo } from "@/features/videos/queries";

const subscription = vi.hoisted(() => ({
	onData: undefined as (() => unknown) | undefined,
}));
vi.mock("@trpc/tanstack-react-query", async (original) => ({
	...(await original<typeof import("@trpc/tanstack-react-query")>()),
	useSubscription: (options: { onData: () => unknown }) => {
		subscription.onData = options.onData;
	},
}));

afterEach(cleanup);

const surfaces = [
	{
		name: "recording status",
		useFeed: useLiveVideoChanges,
		useSnapshot: () => useVideo(1),
		stale: { id: 1, status: "RUNNING" },
		current: { id: 1, status: "DONE" },
	},
	{
		name: "archive queue",
		useFeed: useLiveArchiveQueue,
		useSnapshot: useArchiveQueue,
		stale: { queue: [{ id: 95, status: "PENDING" }], failures: [] },
		current: { queue: [], failures: [] },
	},
	{
		name: "task list",
		useFeed: useLiveTaskStatus,
		useSnapshot: useTasks,
		stale: [{ name: "storage_scan", last_status: "running" }],
		current: [{ name: "storage_scan", last_status: "success" }],
	},
	{
		name: "event logs",
		useFeed: useLiveSystemEvents,
		useSnapshot: () => useEventLogs({ limit: 20, offset: 0 }),
		stale: { total: 0, data: [] },
		current: { total: 1, data: [{ id: 1, message: "Scan finished" }] },
	},
];

it.each(
	surfaces,
)("refreshes $name when a transition arrives during its initial snapshot", async ({
	useFeed,
	useSnapshot,
	stale,
	current,
}) => {
	const pending: ((data: unknown) => void)[] = [];
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: 0 } },
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: () =>
					new Promise<Response>((resolve) => {
						pending.push((data) =>
							resolve(
								new Response(JSON.stringify({ result: { data } }), {
									headers: { "Content-Type": "application/json" },
								}),
							),
						);
					}),
			}),
		],
	});
	const { result, unmount } = renderHook(
		() => {
			useFeed();
			return useSnapshot();
		},
		{
			wrapper: ({ children }) => (
				<QueryClientProvider client={queryClient}>
					<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
						{children}
					</TRPCProvider>
				</QueryClientProvider>
			),
		},
	);
	await waitFor(() => expect(pending).toHaveLength(1));
	act(() => {
		void subscription.onData?.();
	});
	await waitFor(() => expect(pending).toHaveLength(2));
	// An ordinary invalidation reuses the old initial request; a live
	// transition needs a snapshot that starts after the change committed.
	act(() => pending[1](current));
	await waitFor(() => expect(result.current.data).toEqual(current));
	await act(async () => {
		pending[0](stale);
		await new Promise((resolve) => setTimeout(resolve, 10));
	});
	expect(result.current.data).toEqual(current);
	unmount();
	queryClient.clear();
});

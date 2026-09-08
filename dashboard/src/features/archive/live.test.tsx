// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";

const subscriptions = vi.hoisted(() => ({
	onData: [] as (() => void)[],
}));
vi.mock("@trpc/tanstack-react-query", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("@trpc/tanstack-react-query")>();
	return {
		...actual,
		useSubscription: (opts: { onData: () => void }) => {
			subscriptions.onData.push(opts.onData);
		},
	};
});

import { useArchiveQueue, useLiveArchiveQueue } from "./queries";

afterEach(() => {
	cleanup();
	subscriptions.onData = [];
});

// The queue has no poll left: only a pushed membership change refetches it.
describe("useLiveArchiveQueue", () => {
	it("refetches the queue on a pushed change and never on a timer", async () => {
		let fetches = 0;
		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false, gcTime: 0 } },
		});
		const trpcClient = createTRPCClient<AppRouter>({
			links: [
				httpLink({
					url: "http://example.test/trpc",
					fetch: async () => {
						fetches++;
						return new Response(JSON.stringify({ result: { data: [] } }), {
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
		const { result } = renderHook(
			() => {
				useLiveArchiveQueue();
				return useArchiveQueue();
			},
			{ wrapper },
		);
		await waitFor(() => expect(result.current.data).toEqual([]));
		expect(fetches).toBe(1);
		const [query] = queryClient.getQueryCache().findAll();
		expect(
			(query.options as { refetchInterval?: unknown }).refetchInterval,
		).toBeUndefined();
		act(() => subscriptions.onData.at(-1)?.());
		await waitFor(() => expect(fetches).toBe(2));
	});
});

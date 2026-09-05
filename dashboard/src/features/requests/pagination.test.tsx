// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { useAllScheduleRequests, useMyScheduleRequests } from "./queries";

afterEach(cleanup);

describe("request history pagination", () => {
	for (const useRequests of [useMyScheduleRequests, useAllScheduleRequests]) {
		it(`${useRequests.name} follows cursors, preserves rows on failure, and retries`, async () => {
			const cursor = { id: 7, created_at: "2026-06-01T12:00:00Z" };
			let failNext = true;
			const inputs: unknown[] = [];
			const client = new QueryClient({
				defaultOptions: { queries: { retry: false, gcTime: 0 } },
			});
			const trpcClient = createTRPCClient<AppRouter>({
				links: [
					httpLink({
						url: "http://example.test/trpc",
						fetch: async (url) => {
							const input = JSON.parse(
								new URL(String(url)).searchParams.get("input") ?? "{}",
							);
							inputs.push(input);
							if (input.cursor && failNext)
								throw new Error("next page unavailable");
							const data = input.cursor
								? { items: [{ id: 6 }] }
								: { items: [{ id: 8 }, { id: 7 }], next_cursor: cursor };
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
					{ client },
					createElement(TRPCProvider, {
						trpcClient,
						queryClient: client,
						children,
					}),
				);
			const { result, unmount } = renderHook(() => ({ ...useRequests() }), {
				wrapper,
			});
			await waitFor(() =>
				expect(result.current.data).toEqual([{ id: 8 }, { id: 7 }]),
			);
			expect(result.current.hasNextPage).toBe(true);
			await act(async () => {
				await result.current.fetchNextPage();
			});
			expect(inputs).toContainEqual(expect.objectContaining({ cursor }));
			await waitFor(() =>
				expect(result.current.isFetchNextPageError).toBe(true),
			);
			expect(result.current.data).toEqual([{ id: 8 }, { id: 7 }]);
			failNext = false;
			await act(async () => {
				await result.current.fetchNextPage();
			});
			await waitFor(() =>
				expect(result.current.data).toEqual([{ id: 8 }, { id: 7 }, { id: 6 }]),
			);
			expect(result.current.hasNextPage).toBe(false);
			expect(inputs).toEqual([
				{ limit: 50, direction: "forward" },
				{ limit: 50, cursor, direction: "forward" },
				{ limit: 50, cursor, direction: "forward" },
			]);
			unmount();
			client.clear();
		});
	}
});

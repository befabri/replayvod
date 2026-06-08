// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useSetSchedulesPaused, useToggleSchedule } from "./queries";

// Shared, hoisted toggle so the mocked mutationFn can be made to fail per test
// (vi.mock is hoisted above module scope, so it can't close over a plain let).
const mockState = vi.hoisted(() => ({ fail: false }));

// Mirror the tRPC proxy: every family resolves a path-prefix key, and the two
// mutations spread the caller's optimistic handlers onto a mockable mutationFn.
vi.mock("@/api/trpc", () => ({
	useTRPC: () => ({
		schedule: {
			list: { pathKey: () => [["schedule", "list"]] },
			mine: { pathKey: () => [["schedule", "mine"]] },
			pauseState: { pathKey: () => [["schedule", "pauseState"]] },
			toggle: {
				mutationOptions: (options: Record<string, unknown>) => ({
					mutationFn: async () => {
						if (mockState.fail) throw new Error("boom");
						return { id: 1, is_disabled: true };
					},
					...options,
				}),
			},
			setPaused: {
				mutationOptions: (options: Record<string, unknown>) => ({
					mutationFn: async (vars: { paused: boolean }) => {
						if (mockState.fail) throw new Error("boom");
						return { paused: vars.paused };
					},
					...options,
				}),
			},
		},
	}),
}));

function wrapperFor(queryClient: QueryClient) {
	return ({ children }: { children: ReactNode }) =>
		createElement(QueryClientProvider, { client: queryClient }, children);
}

// The real cache entry the schedules page populates: a { data: [...] } envelope
// keyed by { limit, offset } input. The toggle bug was that the optimistic patch
// keyed without input and never reached this entry.
const LIST_KEY = [
	["schedule", "list"],
	{ input: { limit: 50, offset: 0 }, type: "query" },
];
const PAUSE_KEY = [["schedule", "pauseState"], { type: "query" }];

describe("useToggleSchedule", () => {
	it("optimistically flips is_disabled in the input-keyed list cache", async () => {
		mockState.fail = false;
		const queryClient = new QueryClient({
			defaultOptions: { mutations: { retry: false } },
		});
		queryClient.setQueryData(LIST_KEY, {
			data: [{ id: 1, is_disabled: false }],
		});
		const { result } = renderHook(() => useToggleSchedule(), {
			wrapper: wrapperFor(queryClient),
		});

		act(() => {
			result.current.mutate({ id: 1 });
		});

		await waitFor(() => {
			const list = queryClient.getQueryData<{
				data: { id: number; is_disabled: boolean }[];
			}>(LIST_KEY);
			expect(list?.data[0]?.is_disabled).toBe(true);
		});
	});

	it("rolls the flip back when the mutation fails", async () => {
		mockState.fail = true;
		const queryClient = new QueryClient({
			defaultOptions: { mutations: { retry: false } },
		});
		queryClient.setQueryData(LIST_KEY, {
			data: [{ id: 1, is_disabled: false }],
		});
		const { result } = renderHook(() => useToggleSchedule(), {
			wrapper: wrapperFor(queryClient),
		});

		act(() => {
			result.current.mutate({ id: 1 });
		});

		await waitFor(() => {
			expect(result.current.isError).toBe(true);
		});
		const list = queryClient.getQueryData<{
			data: { id: number; is_disabled: boolean }[];
		}>(LIST_KEY);
		expect(list?.data[0]?.is_disabled).toBe(false);
	});
});

describe("useSetSchedulesPaused", () => {
	it("optimistically flips the cached pause flag and invalidates on settle", async () => {
		mockState.fail = false;
		const queryClient = new QueryClient({
			defaultOptions: { mutations: { retry: false } },
		});
		queryClient.setQueryData(PAUSE_KEY, { paused: false });
		const invalidate = vi.spyOn(queryClient, "invalidateQueries");
		const { result } = renderHook(() => useSetSchedulesPaused(), {
			wrapper: wrapperFor(queryClient),
		});

		act(() => {
			result.current.mutate({ paused: true });
		});

		await waitFor(() => {
			expect(queryClient.getQueryData(PAUSE_KEY)).toEqual({ paused: true });
		});
		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: [["schedule", "pauseState"]],
		});
	});

	it("rolls back the cached pause flag when the mutation fails", async () => {
		mockState.fail = true;
		const queryClient = new QueryClient({
			defaultOptions: { mutations: { retry: false } },
		});
		queryClient.setQueryData(PAUSE_KEY, { paused: false });
		const { result } = renderHook(() => useSetSchedulesPaused(), {
			wrapper: wrapperFor(queryClient),
		});

		act(() => {
			result.current.mutate({ paused: true });
		});

		await waitFor(() => {
			expect(result.current.isError).toBe(true);
		});
		expect(queryClient.getQueryData(PAUSE_KEY)).toEqual({ paused: false });
	});
});

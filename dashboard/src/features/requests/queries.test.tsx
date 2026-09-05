// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { ScheduleRequestResponse } from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { useMineSchedules, useSchedules } from "@/features/schedules/queries";
import {
	useAllScheduleRequests,
	useApproveScheduleRequest,
	useCancelScheduleRequest,
	useCreateScheduleRequest,
	useMyScheduleRequests,
	useRejectScheduleRequest,
} from "./queries";

afterEach(cleanup);

const request: ScheduleRequestResponse = {
	id: 7,
	broadcaster_id: "123456",
	broadcaster_login: "channel",
	broadcaster_name: "Channel",
	requested_by: "viewer",
	requested_by_name: "Viewer",
	status: "PENDING",
	created_at: "2026-06-01T12:00:00Z",
};

function useRequestListsAndMutations() {
	return {
		mine: useMyScheduleRequests(),
		queue: useAllScheduleRequests(),
		schedules: useSchedules(),
		nextPage: useSchedules(50, 50),
		mineSchedules: useMineSchedules(),
		create: useCreateScheduleRequest(),
		cancel: useCancelScheduleRequest(),
		reject: useRejectScheduleRequest(),
		approve: useApproveScheduleRequest(),
	};
}

const cases = [
	{
		action: "create",
		initial: [],
		next: [request],
		submit: (h: ReturnType<typeof useRequestListsAndMutations>) =>
			h.create.mutate({ broadcaster_id: request.broadcaster_id }),
	},
	{
		action: "cancel",
		initial: [request],
		next: [],
		submit: (h: ReturnType<typeof useRequestListsAndMutations>) =>
			h.cancel.mutate({ id: request.id }),
	},
	{
		action: "reject",
		initial: [request],
		next: [{ ...request, status: "REJECTED" }],
		submit: (h: ReturnType<typeof useRequestListsAndMutations>) =>
			h.reject.mutate({ id: request.id }),
	},
	{
		action: "approve",
		initial: [request],
		next: [{ ...request, status: "APPROVED", schedule_id: 91 }],
		submit: (h: ReturnType<typeof useRequestListsAndMutations>) =>
			h.approve.mutate({
				request_id: request.id,
				quality: "HIGH",
				has_min_viewers: false,
				has_categories: false,
				has_tags: false,
				is_delete_rediff: false,
				is_disabled: false,
				category_ids: [],
				tag_ids: [],
			}),
	},
] as const;

describe("request mutations refresh mounted lists", () => {
	for (const tc of cases) {
		it.each([false, true])(`${tc.action}: failed=%s`, async (fail) => {
			let changed = false;
			const queryCalls: string[] = [];
			const mutationCalls: { procedure: string | undefined; input: unknown }[] =
				[];
			const client = new QueryClient({
				defaultOptions: {
					queries: { retry: false, staleTime: Infinity, gcTime: 0 },
					mutations: { retry: false },
				},
			});
			// Exercise the real tRPC proxy and cache keys; only HTTP is stubbed.
			const trpcClient = createTRPCClient<AppRouter>({
				links: [
					httpLink({
						url: "http://example.test/trpc",
						fetch: async (url, options) => {
							const proc = new URL(String(url)).pathname.split("/").at(-1);
							let data: unknown;
							if (options?.method === "POST") {
								mutationCalls.push({
									procedure: proc,
									input: JSON.parse(String(options.body)),
								});
								if (fail) throw new Error("request failed");
								changed = true;
								data = tc.action === "approve" ? { id: 91 } : { ok: true };
							} else {
								queryCalls.push(proc ?? "");
								data =
									proc === "schedule.myRequests" || proc === "schedule.requests"
										? { items: changed ? tc.next : tc.initial }
										: {
												data:
													changed && tc.action === "approve"
														? [{ id: 91 }]
														: [],
											};
							}
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
			const { result, unmount } = renderHook(useRequestListsAndMutations, {
				wrapper,
			});
			await waitFor(() => {
				expect(result.current.mine.data).toEqual(tc.initial);
				expect(result.current.queue.data).toEqual(tc.initial);
				expect(result.current.schedules.data).toEqual({ data: [] });
				expect(result.current.nextPage.data).toEqual({ data: [] });
				expect(result.current.mineSchedules.data).toEqual({ data: [] });
			});
			queryCalls.length = 0;
			act(() => tc.submit(result.current));
			await waitFor(() =>
				expect(result.current[tc.action].status).toBe(
					fail ? "error" : "success",
				),
			);
			await waitFor(() => {
				expect(result.current.mine.data).toEqual(fail ? tc.initial : tc.next);
				expect(result.current.queue.data).toEqual(fail ? tc.initial : tc.next);
				const schedules = {
					data: !fail && tc.action === "approve" ? [{ id: 91 }] : [],
				};
				expect(result.current.schedules.data).toEqual(schedules);
				expect(result.current.nextPage.data).toEqual(schedules);
				expect(result.current.mineSchedules.data).toEqual(schedules);
			});
			if (fail) expect(queryCalls).toEqual([]);
			expect(mutationCalls).toMatchObject([
				{
					procedure: `schedule.${tc.action}Request`,
					input:
						tc.action === "create"
							? { broadcaster_id: request.broadcaster_id }
							: tc.action === "approve"
								? { request_id: request.id }
								: { id: request.id },
				},
			]);
			unmount();
			client.clear();
		});
	}
});

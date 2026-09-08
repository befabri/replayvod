// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import {
	useCreateInvite,
	useInvites,
	useRevokeInvite,
	useRotateInvite,
} from "./queries";

afterEach(cleanup);

const invite = {
	id: 7,
	role: "viewer",
	created_by: "admin",
	created_at: "2026-06-01T12:00:00Z",
	expires_at: "2026-06-02T12:00:00Z",
};

// harness wires a hook under test to a fake tRPC transport; fetch decides
// what each procedure returns.
function harness(
	fetch: (url: string, options?: RequestInit) => Promise<Response>,
) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Infinity, gcTime: 0 },
			mutations: { retry: false },
		},
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: (url, options) => fetch(String(url), options),
			}),
		],
	});
	const wrapper = ({ children }: { children: ReactNode }) =>
		createElement(
			QueryClientProvider,
			{ client: queryClient },
			createElement(TRPCProvider, { trpcClient, queryClient, children }),
		);
	return { wrapper, queryClient };
}

function json(data: unknown) {
	return new Response(JSON.stringify({ result: { data } }), {
		headers: { "Content-Type": "application/json" },
	});
}

describe("invite mutations refresh the list", () => {
	for (const action of ["create", "revoke"] as const) {
		it.each([false, true])(`${action}: failed=%s`, async (fail) => {
			let changed = false;
			let listCalls = 0;
			const mutationCalls: { procedure: string; input: unknown }[] = [];
			const initial = action === "create" ? [] : [invite];
			const next = action === "create" ? [invite] : [];
			const created = {
				id: invite.id,
				role: invite.role,
				expires_at: invite.expires_at,
				url: "https://example.test/invite/raw-token",
			};
			const { wrapper, queryClient } = harness(async (url, options) => {
				const procedure = new URL(url).pathname;
				if (options?.method === "POST") {
					mutationCalls.push({
						procedure,
						input: JSON.parse(String(options.body)),
					});
					if (fail) throw new Error("invite request failed");
					changed = true;
					return json(action === "create" ? created : { ok: true });
				}
				expect(procedure).toBe("/trpc/system.listInvites");
				listCalls++;
				return json(changed ? next : initial);
			});
			const { result, unmount } = renderHook(
				() => ({
					list: useInvites(),
					create: useCreateInvite(),
					revoke: useRevokeInvite(),
				}),
				{ wrapper },
			);
			await waitFor(() => expect(result.current.list.data).toEqual(initial));
			act(() => {
				if (action === "create")
					result.current.create.mutate({ role: "viewer", ttl_minutes: 60 });
				else result.current.revoke.mutate({ id: invite.id });
			});
			await waitFor(() =>
				expect(result.current[action].status).toBe(fail ? "error" : "success"),
			);
			await waitFor(() =>
				expect(result.current.list.data).toEqual(fail ? initial : next),
			);
			if (fail) expect(listCalls).toBe(1);
			expect(mutationCalls).toEqual([
				{
					procedure: `/trpc/system.${action}Invite`,
					input:
						action === "create"
							? { role: "viewer", ttl_minutes: 60 }
							: { id: invite.id },
				},
			]);
			if (action === "create" && !fail) {
				expect(result.current.create.data).toEqual(created);
				expect(result.current.list.data?.[0]).not.toHaveProperty("url");
			}
			unmount();
			queryClient.clear();
		});
	}
});

describe("rotating an invite", () => {
	it.each([
		false,
		true,
	])("failed=%s: posts the id, returns the fresh link once, and refreshes the list", async (fail) => {
		let listCalls = 0;
		const mutationCalls: { procedure: string; input: unknown }[] = [];
		const fresh = {
			id: invite.id,
			role: invite.role,
			expires_at: invite.expires_at,
			url: "https://example.test/invite/fresh-token",
		};
		const { wrapper, queryClient } = harness(async (url, options) => {
			const procedure = new URL(url).pathname;
			if (options?.method === "POST") {
				mutationCalls.push({
					procedure,
					input: JSON.parse(String(options.body)),
				});
				if (fail) throw new Error("invite not found");
				return json(fresh);
			}
			expect(procedure).toBe("/trpc/system.listInvites");
			listCalls++;
			return json([invite]);
		});
		const { result, unmount } = renderHook(
			() => ({ list: useInvites(), rotate: useRotateInvite() }),
			{ wrapper },
		);
		await waitFor(() => expect(result.current.list.data).toEqual([invite]));
		act(() => result.current.rotate.mutate({ id: invite.id }));
		await waitFor(() =>
			expect(result.current.rotate.status).toBe(fail ? "error" : "success"),
		);
		// A rejected rotate means the row changed underneath; both outcomes
		// refetch so the table reflects the server.
		await waitFor(() => expect(listCalls).toBe(2));
		expect(mutationCalls).toEqual([
			{ procedure: "/trpc/system.rotateInvite", input: { id: invite.id } },
		]);
		if (!fail) expect(result.current.rotate.data).toEqual(fresh);
		expect(result.current.list.data?.[0]).not.toHaveProperty("url");
		unmount();
		queryClient.clear();
	});
});

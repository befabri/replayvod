// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { useCreateInvite, useInvites, useRevokeInvite } from "./queries";

afterEach(cleanup);

const invite = {
	id: 7,
	role: "viewer",
	created_by: "admin",
	created_at: "2026-06-01T12:00:00Z",
	expires_at: "2026-06-02T12:00:00Z",
};

describe("invite mutations refresh the list", () => {
	for (const action of ["create", "revoke"] as const) {
		it.each([false, true])(`${action}: failed=%s`, async (fail) => {
			let changed = false;
			let listCalls = 0;
			const mutationCalls: { procedure: string; input: unknown }[] = [];
			const initial = action === "create" ? [] : [invite];
			const next = action === "create" ? [invite] : [];
			const queryClient = new QueryClient({
				defaultOptions: {
					queries: { retry: false, staleTime: Infinity, gcTime: 0 },
					mutations: { retry: false },
				},
			});
			const created = {
				id: invite.id,
				role: invite.role,
				expires_at: invite.expires_at,
				url: "https://example.test/invite/raw-token",
			};
			const trpcClient = createTRPCClient<AppRouter>({
				links: [
					httpLink({
						url: "http://example.test/trpc",
						fetch: async (url, options) => {
							const procedure = new URL(String(url)).pathname;
							let data: unknown;
							if (options?.method === "POST") {
								mutationCalls.push({
									procedure,
									input: JSON.parse(String(options.body)),
								});
								if (fail) throw new Error("invite request failed");
								changed = true;
								data = action === "create" ? created : { ok: true };
							} else {
								expect(procedure).toBe("/trpc/system.listInvites");
								listCalls++;
								data = changed ? next : initial;
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
					{ client: queryClient },
					createElement(TRPCProvider, { trpcClient, queryClient, children }),
				);
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

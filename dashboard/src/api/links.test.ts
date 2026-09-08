import { createTRPCClient } from "@trpc/client";
import { describe, expect, it, vi } from "vitest";
import type { AppRouter } from "@/api/trpc";
import { dashboardLinks } from "./links";

describe("dashboardLinks credential transport", () => {
	it("refuses credential submission before the real batch transport sees it", async () => {
		const fetch = vi.fn();
		const client = createTRPCClient<AppRouter>({
			links: dashboardLinks({
				apiUrl: "https://api.example",
				credentialsAllowed: () => false,
				fetch,
			}),
		});
		await expect(
			client.twitchPlayback.connect.mutate({
				session_token: "synthetic-session-0123456789abcdef",
				consent: true,
			}),
		).rejects.toThrow("Use HTTPS");
		expect(fetch).not.toHaveBeenCalled();
	});
	it("keeps cookies on RPC requests and refuses redirects that could forward credentials", async () => {
		const fetch = vi.fn(
			async () =>
				new Response(
					JSON.stringify([
						{
							result: {
								data: {
									state: "connected",
									login: "viewer",
									expires_at: 0,
									checked_at: 0,
								},
							},
						},
					]),
					{ headers: { "Content-Type": "application/json" } },
				),
		);
		const client = createTRPCClient<AppRouter>({
			links: dashboardLinks({
				apiUrl: "https://api.example",
				credentialsAllowed: () => true,
				fetch,
			}),
		});
		await client.twitchPlayback.connect.mutate({
			session_token: "synthetic-session-0123456789abcdef",
			consent: true,
		});
		expect(fetch).toHaveBeenCalledExactlyOnceWith(
			expect.stringContaining("twitchPlayback.connect"),
			expect.objectContaining({
				method: "POST",
				credentials: "include",
				redirect: "error",
			}),
		);
	});
});

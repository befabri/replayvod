import { createTRPCClient } from "@trpc/client";
import { describe, expect, it, vi } from "vitest";
import type { AppRouter } from "@/api/trpc";
import { dashboardLinks, KEEPALIVE_CONTEXT } from "./links";

const state = {
	watch_later: false,
	last_position_seconds: 42,
	updated_at: "2026-01-01T00:00:00Z",
};

describe("dashboardLinks", () => {
	it("sends a keepalive mutation on its own keepalive request and batches the rest", async () => {
		const calls: { url: string; init: RequestInit }[] = [];
		const fetchImpl = vi.fn(
			async (input: RequestInfo | URL, init?: RequestInit) => {
				calls.push({ url: String(input), init: init ?? {} });
				const url = new URL(String(input));
				const envelope = { result: { data: state } };
				const body = url.searchParams.has("batch")
					? url.pathname
							.replace(/^.*\/trpc\//, "")
							.split(",")
							.map(() => envelope)
					: envelope;
				return new Response(JSON.stringify(body), {
					headers: { "Content-Type": "application/json" },
				});
			},
		);
		const client = createTRPCClient<AppRouter>({
			links: dashboardLinks({
				apiUrl: "http://replay.test",
				credentialsAllowed: () => true,
				fetch: fetchImpl as unknown as typeof fetch,
			}),
		});
		const input = {
			video_id: 7,
			position_seconds: 42,
			completed: false,
		};

		await client.video.updateWatchProgress.mutate(input, {
			context: KEEPALIVE_CONTEXT,
		});
		await client.video.updateWatchProgress.mutate(input);
		await client.video.getById.query({ id: 7 });

		expect(calls).toHaveLength(3);
		const [keepalive, batchedMutation, batchedQuery] = calls;
		expect(keepalive?.url).toBe(
			"http://replay.test/trpc/video.updateWatchProgress",
		);
		expect(keepalive?.init).toMatchObject({
			method: "POST",
			credentials: "include",
			keepalive: true,
		});
		expect(JSON.parse(String(keepalive?.init.body))).toEqual(input);
		for (const call of [batchedMutation, batchedQuery]) {
			expect(call?.url).toContain("batch=1");
			expect(call?.init.credentials).toBe("include");
			expect(call?.init.keepalive).toBeUndefined();
		}
	});
});

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

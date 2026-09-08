import { createTRPCClient, httpLink } from "@trpc/client";
import { describe, expect, it, vi } from "vitest";
import type { AppRouter } from "@/api/trpc";
import {
	credentialTransportAllowed,
	credentialTransportLink,
} from "./credential-transport";

describe("credential transport", () => {
	it.each([
		["https://replay.example/dashboard", "", true],
		["https://replay.example", "https://api.example", true],
		["http://localhost:3000", "http://127.0.0.1:8080", true],
		["http://[::1]:3000", "", true],
		["http://127.0.0.2:3000", "", true],
		["http://192.168.1.20:3000", "", false],
		["http://replay.example", "https://api.example", false],
		["https://replay.example", "http://api.example", false],
		["http://localhost.evil", "", false],
		["http://localhost", "http://localhost.evil", false],
		["https://replay.example", "javascript:alert(1)", false],
		["https://replay.example", "https://user:password@api.example", false],
		["not a URL", "", false],
		["https://replay.example", "https://[", false],
	])("page %s, API %s: %s", (page, api, allowed) => {
		expect(credentialTransportAllowed(page, api)).toBe(allowed);
	});
	it("blocks credential mutations before batching or fetch, while status and disconnect remain available", async () => {
		const fetch = vi.fn(
			async () =>
				new Response(
					JSON.stringify({
						result: {
							data: {
								state: "disconnected",
								login: "",
								checked_at: 0,
								expires_at: 0,
							},
						},
					}),
					{ headers: { "Content-Type": "application/json" } },
				),
		);
		let allowed = false;
		const client = createTRPCClient<AppRouter>({
			links: [
				credentialTransportLink(() => allowed),
				httpLink({ url: "http://replay.example/trpc", fetch }),
			],
		});
		await expect(
			client.twitchPlayback.connect.mutate({
				session_token: "synthetic-session-0123456789abcdef",
				consent: true,
			}),
		).rejects.toThrow("Use HTTPS");
		expect(fetch).not.toHaveBeenCalled();
		await client.twitchPlayback.status.query();
		await client.twitchPlayback.disconnect.mutate();
		expect(fetch).toHaveBeenCalledTimes(2);
		allowed = true;
		await client.twitchPlayback.connect.mutate({
			session_token: "synthetic-session-0123456789abcdef",
			consent: true,
		});
		expect(fetch).toHaveBeenCalledTimes(3);
	});
});

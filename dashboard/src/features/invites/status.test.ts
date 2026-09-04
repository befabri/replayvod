import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InviteInfo } from "@/api/generated/trpc";
import { inviteStatus } from "./status";

function invite(overrides: Partial<InviteInfo>): InviteInfo {
	return {
		id: 1,
		role: "viewer",
		created_by: "owner-1",
		expires_at: "2026-01-02T00:00:00Z",
		created_at: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

describe("inviteStatus", () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("is pending while unredeemed and before expiry", () => {
		expect(inviteStatus(invite({}))).toBe("pending");
	});

	it("is expired once the TTL passed", () => {
		expect(inviteStatus(invite({ expires_at: "2026-01-01T11:59:59Z" }))).toBe(
			"expired",
		);
	});

	it("is expired at the exact expiry instant", () => {
		expect(inviteStatus(invite({ expires_at: "2026-01-01T12:00:00Z" }))).toBe(
			"expired",
		);
	});

	it("is redeemed when redeemed_at is set, even past expiry", () => {
		expect(
			inviteStatus(
				invite({
					expires_at: "2026-01-01T00:30:00Z",
					redeemed_at: "2026-01-01T00:10:00Z",
					redeemed_by: "twitch-99",
				}),
			),
		).toBe("redeemed");
	});
});

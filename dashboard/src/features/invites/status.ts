import type { InviteInfo } from "@/api/generated/trpc";

export type InviteStatus = "pending" | "redeemed" | "expired";

// Status is derived, not stored: a row is redeemed once redeemed_at is
// set, expired once its TTL passed, otherwise still pending.
export function inviteStatus(invite: InviteInfo): InviteStatus {
	if (invite.redeemed_at) return "redeemed";
	if (new Date(invite.expires_at).getTime() <= Date.now()) return "expired";
	return "pending";
}

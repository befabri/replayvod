import type { InviteInfo } from "@/api/generated/trpc";

export type InviteStatus = "pending" | "redeemed" | "expired";

export function inviteStatus(invite: InviteInfo): InviteStatus {
	if (invite.redeemed_at) return "redeemed";
	if (new Date(invite.expires_at).getTime() <= Date.now()) return "expired";
	return "pending";
}

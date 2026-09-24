import type {
	ConfigResponse,
	SnapshotResponse,
	SubscriptionResponse,
} from "@/api/generated/trpc";
import { FIXTURE_NOW } from "./videos";

export function makeSnapshot(
	index = 0,
	overrides: Partial<SnapshotResponse> = {},
): SnapshotResponse {
	const total = 18 + ((index * 7) % 13);
	return {
		id: index + 1,
		total,
		total_cost: total * 2,
		max_total_cost: 10_000,
		fetched_at: new Date(FIXTURE_NOW - index * 6 * 3_600_000).toISOString(),
		...overrides,
	};
}

export function makeSnapshots(count: number): SnapshotResponse[] {
	return Array.from({ length: count }, (_, index) => makeSnapshot(index));
}

export function makeEventSubConfig(
	overrides: Partial<ConfigResponse> = {},
): ConfigResponse {
	return {
		source: "database",
		mode: "poll",
		env_managed: false,
		setup_required: false,
		restart_required: false,
		creates_twitch_subscriptions: false,
		uses_relay_agent: false,
		polls_helix: true,
		direct_callback_url:
			"https://replayvod.example.com/api/v1/webhook/callback",
		active: {
			source: "database",
			mode: "poll",
			creates_twitch_subscriptions: false,
			uses_relay_agent: false,
			polls_helix: true,
		},
		...overrides,
	};
}

export function makeSubscription(
	index = 0,
	overrides: Partial<SubscriptionResponse> = {},
): SubscriptionResponse {
	const created = new Date(FIXTURE_NOW - index * 3_600_000).toISOString();
	return {
		id: `sub-${(index + 1).toString().padStart(4, "0")}`,
		status: "enabled",
		type: index % 2 === 0 ? "stream.online" : "stream.offline",
		version: "1",
		cost: 1,
		condition: { broadcaster_user_id: `${41_000_000 + index}` },
		broadcaster_id: `${41_000_000 + index}`,
		transport_method: "webhook",
		transport_callback: "https://replayvod.example.com/api/v1/webhook/callback",
		twitch_created_at: created,
		created_at: created,
		...overrides,
	};
}

export function makeSubscriptions(count: number): SubscriptionResponse[] {
	return Array.from({ length: count }, (_, index) => makeSubscription(index));
}

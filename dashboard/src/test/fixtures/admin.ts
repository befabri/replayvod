import type {
	InviteInfo,
	SessionInfo,
	TaskResponse,
	WhitelistEntryInfo,
} from "@/api/generated/trpc";
import { CURRENT_USER, USERS } from "./users";
import { FIXTURE_NOW } from "./videos";

const MINUTE_MS = 60_000;
const DAY_MS = 24 * 60 * MINUTE_MS;

function at(offsetMs: number) {
	return new Date(FIXTURE_NOW + offsetMs).toISOString();
}

const TASK_SPECS = [
	{
		name: "thumbnail_backfill",
		description: "Generates missing thumbnails for finished recordings.",
		interval_seconds: 3600,
		last_status: "success",
	},
	{
		name: "storage_scan",
		description: "Marks recordings whose media left the storage as missing.",
		interval_seconds: 900,
		last_status: "running",
	},
	{
		name: "eventsub_sync",
		description: "Recreates EventSub subscriptions Twitch dropped.",
		interval_seconds: 300,
		last_status: "failed",
		last_error: "twitch returned 503",
	},
	{
		name: "playback_cache_trim",
		description: "Evicts generated playback files over the cache limit.",
		interval_seconds: 0,
		last_status: "skipped",
	},
	{
		name: "session_cleanup",
		description: "Deletes expired dashboard sessions.",
		interval_seconds: 86_400,
		last_status: "success",
		is_enabled: false,
	},
];

export function makeTasks(): TaskResponse[] {
	return TASK_SPECS.map((spec, index) => ({
		is_enabled: true,
		is_available: true,
		last_duration_ms: 120 + index * 45,
		last_run_at: at(-(index + 1) * 7 * MINUTE_MS),
		next_run_at:
			spec.interval_seconds > 0
				? at(spec.interval_seconds * 1000 - index * MINUTE_MS)
				: undefined,
		created_at: at(-30 * DAY_MS),
		updated_at: at(-(index + 1) * 7 * MINUTE_MS),
		...spec,
	}));
}

const USER_AGENTS = [
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:142.0) Gecko/20100101 Firefox/142.0",
	"",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15",
];

export function makeSessions(): SessionInfo[] {
	return USER_AGENTS.map((user_agent, index) => ({
		hashed_id: `session-${index + 1}`,
		user_agent: user_agent || undefined,
		ip_address: index === 3 ? undefined : `192.0.2.${10 + index * 7}`,
		current: index === 0,
		created_at: at(-(index + 2) * DAY_MS),
		last_active_at: at(-index * 45 * MINUTE_MS),
		expires_at: at((30 - index) * DAY_MS),
	}));
}

export function makeWhitelistEntries(): WhitelistEntryInfo[] {
	return ["141981764", "26490481", "71092938", "12826", "403106339"].map(
		(twitch_user_id, index) => ({
			twitch_user_id,
			added_at: at(-(index + 1) * 3 * DAY_MS),
		}),
	);
}

type InviteSpec = Omit<InviteInfo, "id" | "created_by" | "created_at">;

export function makeInvites(): InviteInfo[] {
	const [, bob] = USERS;
	const specs: InviteSpec[] = [
		{ note: "Weekend co-host", role: "admin", expires_at: at(DAY_MS) },
		{ role: "viewer", expires_at: at(7 * DAY_MS) },
		{
			note: "Clip editor",
			role: "viewer",
			expires_at: at(-DAY_MS),
			redeemed_at: at(-2 * DAY_MS),
			redeemed_by: bob.id,
		},
		{ note: "Old link", role: "viewer", expires_at: at(-3 * DAY_MS) },
		{ note: "Mod team", role: "admin", expires_at: at(30 * DAY_MS) },
	];
	return specs.map((invite, index) => ({
		id: index + 1,
		created_by: CURRENT_USER.id,
		created_at: at(-(index + 4) * DAY_MS),
		...invite,
	}));
}

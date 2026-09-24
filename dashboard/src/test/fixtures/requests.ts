import type {
	ScheduleRequestResponse,
	ScheduleRequestStatus,
} from "@/api/generated/trpc";
import { channelAt } from "./channels";
import { userAt } from "./users";
import { FIXTURE_NOW } from "./videos";

const NOTES: readonly (string | undefined)[] = [
	"Streams every weekday evening, would love the VODs kept.",
	undefined,
	"Tournament run this weekend.",
	"Mostly audio podcasts, audio only is fine.",
];

const STATUS_CYCLE: readonly ScheduleRequestStatus[] = [
	"PENDING",
	"APPROVED",
	"PENDING",
	"REJECTED",
];

export function makeScheduleRequest(
	index = 0,
	overrides: Partial<ScheduleRequestResponse> = {},
): ScheduleRequestResponse {
	const channel = channelAt(index + 2);
	const requester = userAt(index);
	const status = STATUS_CYCLE[index % STATUS_CYCLE.length];
	const createdAt = new Date(FIXTURE_NOW - (index + 1) * 7 * 3600_000);
	return {
		id: index + 1,
		broadcaster_id: channel.id,
		broadcaster_login: channel.login,
		broadcaster_name: channel.displayName,
		profile_image_url: channel.profileImageUrl,
		requested_by: requester.id,
		requested_by_name: requester.displayName,
		note: NOTES[index % NOTES.length],
		status,
		created_at: createdAt.toISOString(),
		...(status === "PENDING"
			? {}
			: {
					decided_by: "u1",
					decided_at: new Date(createdAt.getTime() + 3600_000).toISOString(),
				}),
		...overrides,
	};
}

export function makeScheduleRequests(count: number): ScheduleRequestResponse[] {
	return Array.from({ length: count }, (_, index) =>
		makeScheduleRequest(index),
	);
}

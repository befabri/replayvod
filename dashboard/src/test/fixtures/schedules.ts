import type { ScheduleResponse } from "@/api/generated/trpc";
import { RECORDING_QUALITIES } from "@/lib/recording-settings";
import { categoryAt } from "./categories";
import { channelAt } from "./channels";
import { TAGS } from "./tags";
import { CURRENT_USER } from "./users";
import { FIXTURE_NOW } from "./videos";

export function makeSchedule(
	index = 0,
	overrides: Partial<ScheduleResponse> = {},
): ScheduleResponse {
	const channel = channelAt(index);
	const audio = index % 4 === 3;
	const createdAt = new Date(FIXTURE_NOW - (index + 3) * 86_400_000);
	return {
		id: index + 1,
		broadcaster_id: channel.id,
		requested_by: CURRENT_USER.id,
		requested_from_name: "",
		recording_type: audio ? "audio" : "video",
		quality: RECORDING_QUALITIES[index % RECORDING_QUALITIES.length],
		force_h264: index % 2 === 0,
		has_min_viewers: index % 3 === 1,
		min_viewers: index % 3 === 1 ? 50 : undefined,
		has_categories: index % 2 === 1,
		has_tags: index % 3 === 2,
		is_delete_rediff: false,
		is_disabled: index % 5 === 4,
		last_triggered_at:
			index % 2 === 0
				? new Date(FIXTURE_NOW - (index + 1) * 3_600_000).toISOString()
				: undefined,
		trigger_count: 3 + index * 4,
		created_at: createdAt.toISOString(),
		updated_at: createdAt.toISOString(),
		categories:
			index % 2 === 1
				? [categoryAt(index), categoryAt(index + 1)].map(({ id, name }) => ({
						id,
						name,
					}))
				: [],
		tags:
			index % 3 === 2
				? [TAGS[index % TAGS.length]].map(({ id, name }) => ({ id, name }))
				: [],
		...overrides,
	};
}

export function makeSchedules(count: number): ScheduleResponse[] {
	return Array.from({ length: count }, (_, index) => makeSchedule(index));
}

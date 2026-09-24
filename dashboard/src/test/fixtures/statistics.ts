import type { StatisticsResponse } from "@/api/generated/trpc";

export function makeVideoStatistics(
	overrides: Partial<StatisticsResponse> = {},
): StatisticsResponse {
	return {
		total: 1725,
		total_size: 912_000_000_000,
		total_duration_seconds: 1_104_000,
		by_status: [
			{ status: "DONE", count: 1712 },
			{ status: "RUNNING", count: 3 },
			{ status: "FAILED", count: 10 },
		],
		this_week: 12,
		incomplete: 4,
		channels: 8,
		removed: 6,
		watch_later: 5,
		unwatched: 40,
		continue_watching: 7,
		...overrides,
	};
}

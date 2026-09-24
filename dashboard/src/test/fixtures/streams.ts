import type { FollowedStreamResponse } from "@/api/generated/trpc";
import { categoryAt } from "./categories";
import { channelAt } from "./channels";
import { FIXTURE_NOW, VOD_TITLES } from "./videos";

export function makeFollowedStream(
	index = 0,
	overrides: Partial<FollowedStreamResponse> = {},
): FollowedStreamResponse {
	const channel = channelAt(index);
	const category = categoryAt(index + 3);
	return {
		stream_id: `stream-${index + 1}`,
		broadcaster_id: channel.id,
		broadcaster_login: channel.login,
		broadcaster_name: channel.displayName,
		profile_image_url: channel.profileImageUrl,
		game_id: category.id,
		game_name: category.name,
		type: "live",
		title: VOD_TITLES[index % VOD_TITLES.length],
		language: index % 3 === 0 ? "en" : "fr",
		viewer_count: 120 + index * 57,
		started_at: new Date(FIXTURE_NOW - (index + 1) * 47 * 60_000).toISOString(),
		...overrides,
	};
}

export function makeFollowedStreams(count: number): FollowedStreamResponse[] {
	return Array.from({ length: count }, (_, index) => makeFollowedStream(index));
}

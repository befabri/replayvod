import type { PlaybackCacheConfigResponse } from "@/api/generated/trpc";

export function makePlaybackCacheConfig(
	overrides: Partial<PlaybackCacheConfigResponse> = {},
): PlaybackCacheConfigResponse {
	return { enabled: true, max_percent: 20, auto_generate: true, ...overrides };
}

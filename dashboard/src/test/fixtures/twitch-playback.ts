import type { TwitchPlaybackStatusResponse } from "@/api/generated/trpc";
import { CHANNELS } from "./channels";
import { FIXTURE_NOW } from "./videos";

const DAY_SECONDS = 24 * 60 * 60;

export function makeTwitchPlaybackStatus(
	state: "connected" | "disconnected" | "reconnect_required" = "connected",
	overrides: Partial<TwitchPlaybackStatusResponse> = {},
): TwitchPlaybackStatusResponse {
	const nowSeconds = Math.floor(FIXTURE_NOW / 1000);
	return {
		state,
		login: state === "disconnected" ? "" : CHANNELS[0].login,
		checked_at: state === "disconnected" ? 0 : nowSeconds - 3600,
		expires_at: state === "disconnected" ? 0 : nowSeconds + 30 * DAY_SECONDS,
		...overrides,
	};
}

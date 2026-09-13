import type { SettingsResponse } from "@/api/generated/trpc";

// Represents the API response for a viewer who has kept the server defaults.
export const PLAYBACK_SETTINGS: SettingsResponse["playback"] = {
	resume_min_seconds: 5,
	resume_end_margin_seconds: 30,
	resume_end_margin_percent: 5,
};

export const USER_SETTINGS: SettingsResponse = {
	user_id: "u1",
	timezone: "UTC",
	datetime_format: "ISO",
	language: "en",
	playback: PLAYBACK_SETTINGS,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
};

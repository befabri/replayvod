import type { ChannelResponse } from "@/api/generated/trpc";

export interface FakeChannel {
	id: string;
	login: string;
	displayName: string;
	profileImageUrl: string;
}

function profileImage(from: string, to: string, letter: string) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="64" height="64" fill="url(#g)"/><text x="32" y="42" font-family="sans-serif" font-size="28" font-weight="700" text-anchor="middle" fill="white">${letter}</text></svg>`;
	return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

function channel(
	id: string,
	displayName: string,
	from: string,
	to: string,
): FakeChannel {
	return {
		id,
		login: displayName.toLowerCase(),
		displayName,
		profileImageUrl: profileImage(from, to, displayName.charAt(0)),
	};
}

export const CHANNELS: readonly FakeChannel[] = [
	channel("1001", "PixelHarbor", "#8390FA", "#33305E"),
	channel("1002", "MoonlitMarmot", "#F97316", "#7C2D12"),
	channel("1003", "CrumbleKnight", "#14B8A6", "#134E4A"),
	channel("1004", "VelvetVortex", "#EC4899", "#831843"),
	channel("1005", "TinyAnvil", "#EAB308", "#713F12"),
	channel("1006", "SleepyQuasar", "#6366F1", "#1E1B4B"),
	channel("1007", "NeonBadger", "#22C55E", "#14532D"),
	channel("1008", "SaltySeagull", "#0EA5E9", "#0C4A6E"),
];

export function channelAt(index: number): FakeChannel {
	return CHANNELS[index % CHANNELS.length];
}

export function makeChannelResponse(
	index = 0,
	overrides: Partial<ChannelResponse> = {},
): ChannelResponse {
	const channel = channelAt(index);
	return {
		broadcaster_id: channel.id,
		broadcaster_login: channel.login,
		broadcaster_name: channel.displayName,
		broadcaster_language: index % 3 === 0 ? "en" : "fr",
		profile_image_url: channel.profileImageUrl,
		description: `${channel.displayName} streams most evenings and keeps the VODs.`,
		view_count: 1200 + index * 317,
		created_at: "2024-03-01T00:00:00Z",
		updated_at: "2026-09-01T00:00:00Z",
		...overrides,
	};
}

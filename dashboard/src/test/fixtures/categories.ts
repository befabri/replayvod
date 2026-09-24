import type { CategoryDetailResponse } from "@/api/generated/trpc";

export interface FakeCategory {
	id: string;
	name: string;
}

export const CATEGORIES: readonly FakeCategory[] = [
	"Just Chatting",
	"League of Legends",
	"VALORANT",
	"Minecraft",
	"Counter-Strike",
	"Fortnite",
	"Rocket League",
	"Elden Ring",
	"Music",
	"Art",
].map((name, index) => ({ id: `category-${index + 1}`, name }));

export function categoryAt(index: number): FakeCategory {
	return CATEGORIES[index % CATEGORIES.length];
}

function boxArt(name: string, hue: number) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 144 192"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue} 70% 55%)"/><stop offset="1" stop-color="hsl(${hue} 60% 20%)"/></linearGradient></defs><rect width="144" height="192" fill="url(#g)"/><text x="72" y="108" font-family="sans-serif" font-size="40" font-weight="700" text-anchor="middle" fill="white">${name.charAt(0)}</text></svg>`;
	return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

export function makeCategoryDetail(
	index = 0,
	overrides: Partial<CategoryDetailResponse> = {},
): CategoryDetailResponse {
	const category = categoryAt(index);
	return {
		id: category.id,
		name: category.name,
		box_art_url: boxArt(category.name, (index * 47) % 360),
		description: `${category.name} recordings from every channel on this server, newest first.`,
		created_at: "2024-03-01T00:00:00Z",
		updated_at: "2026-09-01T00:00:00Z",
		video_count: 12 + index * 5,
		total_size: (12 + index * 5) * 2_400_000_000,
		...overrides,
	};
}

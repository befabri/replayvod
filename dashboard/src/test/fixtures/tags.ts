import type { TagResponse } from "@/api/generated/trpc";

export const TAGS: readonly TagResponse[] = [
	"English",
	"Speedrun",
	"Competitive",
	"Chill",
	"Français",
	"NoBackseating",
	"Tournament",
	"IRL",
	"Coworking",
	"Retro",
].map((name, index) => ({
	id: index + 1,
	name,
	created_at: "2026-01-01T00:00:00Z",
}));

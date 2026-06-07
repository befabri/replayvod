import type { InfiniteData } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
	ChannelPageResponse,
	ChannelResponse,
	ChannelUserStateResponse,
} from "@/api/generated/trpc";
import {
	queryKeyHasChannelListFilter,
	updateChannelPagesFavorite,
} from "./queries";

function channel(partial: Partial<ChannelResponse>): ChannelResponse {
	return {
		broadcaster_id: partial.broadcaster_id ?? "bc-1",
		broadcaster_login: partial.broadcaster_login ?? "channel",
		broadcaster_name: partial.broadcaster_name ?? "Channel",
		view_count: partial.view_count ?? 0,
		created_at: partial.created_at ?? "2026-01-01T00:00:00Z",
		updated_at: partial.updated_at ?? "2026-01-01T00:00:00Z",
		user_state: partial.user_state,
	};
}

function cache(items: ChannelResponse[]): InfiniteData<ChannelPageResponse> {
	return {
		pages: [{ items }],
		pageParams: [undefined],
	};
}

const removedState: ChannelUserStateResponse = {
	favorite: false,
	updated_at: "2026-01-02T00:00:00Z",
};

describe("channel favorite query cache helpers", () => {
	it("updates normal cached lists without removing the channel", () => {
		const next = updateChannelPagesFavorite(
			cache([
				channel({
					broadcaster_id: "bc-1",
					user_state: { ...removedState, favorite: true },
				}),
			]),
			"bc-1",
			removedState,
			{ removeMatching: false },
		);

		expect(next?.pages[0]?.items).toHaveLength(1);
		expect(next?.pages[0]?.items[0]?.user_state?.favorite).toBe(false);
	});

	it("removes the channel from favorite-only cached lists", () => {
		const next = updateChannelPagesFavorite(
			cache([
				channel({
					broadcaster_id: "bc-1",
					user_state: { ...removedState, favorite: true },
				}),
				channel({
					broadcaster_id: "bc-2",
					user_state: { ...removedState, favorite: true },
				}),
			]),
			"bc-1",
			removedState,
			{ removeMatching: true },
		);

		expect(next?.pages[0]?.items.map((item) => item.broadcaster_id)).toEqual([
			"bc-2",
		]);
	});

	it("detects favorite-only tRPC query keys", () => {
		expect(
			queryKeyHasChannelListFilter(
				[
					["channel", "listPage"],
					{ input: { limit: 60, sort: "name_asc", filter: "favorites" } },
				],
				"favorites",
			),
		).toBe(true);
		expect(
			queryKeyHasChannelListFilter(
				[
					["channel", "listPage"],
					{ input: { limit: 60, sort: "name_asc", filter: "downloaded" } },
				],
				"favorites",
			),
		).toBe(false);
	});
});

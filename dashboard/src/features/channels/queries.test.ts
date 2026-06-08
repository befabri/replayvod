import { type InfiniteData, QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
	ChannelPageResponse,
	ChannelResponse,
	ChannelUserStateResponse,
} from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { patchEntity } from "@/lib/query";
import { channelCaches, channelFavoritePatch } from "./cache";

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

function pages(items: ChannelResponse[]): InfiniteData<ChannelPageResponse> {
	return { pages: [{ items }], pageParams: [undefined] };
}

// Mirror the tRPC proxy surface channelCaches reaches into: every family
// resolves a path-prefix key, matching real cache entries regardless of input.
function fakeTrpc(): ReturnType<typeof useTRPC> {
	const node = (name: string) => ({ pathKey: () => [["channel", name]] });
	return {
		channel: {
			list: node("list"),
			listPage: node("listPage"),
			getById: node("getById"),
			getByLogin: node("getByLogin"),
			search: node("search"),
			listFollowed: node("listFollowed"),
		},
	} as unknown as ReturnType<typeof useTRPC>;
}

const removedState: ChannelUserStateResponse = {
	favorite: false,
	updated_at: "2026-01-02T00:00:00Z",
};

function listPageKey(filter: string) {
	return [
		["channel", "listPage"],
		{ input: { limit: 60, sort: "name_asc", filter }, type: "infinite" },
	];
}

describe("channelFavoritePatch via patchEntity", () => {
	it("updates a channel in a normal infinite list without removing it", () => {
		const qc = new QueryClient();
		const caches = channelCaches(fakeTrpc());
		qc.setQueryData(
			listPageKey("all"),
			pages([
				channel({
					broadcaster_id: "bc-1",
					user_state: { ...removedState, favorite: true },
				}),
			]),
		);

		patchEntity(qc, caches, channelFavoritePatch("bc-1", removedState));

		const next = qc.getQueryData<InfiniteData<ChannelPageResponse>>(
			listPageKey("all"),
		);
		expect(next?.pages[0]?.items).toHaveLength(1);
		expect(next?.pages[0]?.items[0]?.user_state?.favorite).toBe(false);
	});

	it("removes an un-favorited channel from a favorites-only infinite list", () => {
		const qc = new QueryClient();
		const caches = channelCaches(fakeTrpc());
		qc.setQueryData(
			listPageKey("favorites"),
			pages([
				channel({
					broadcaster_id: "bc-1",
					user_state: { ...removedState, favorite: true },
				}),
				channel({
					broadcaster_id: "bc-2",
					user_state: { ...removedState, favorite: true },
				}),
			]),
		);

		patchEntity(qc, caches, channelFavoritePatch("bc-1", removedState));

		const next = qc.getQueryData<InfiniteData<ChannelPageResponse>>(
			listPageKey("favorites"),
		);
		expect(next?.pages[0]?.items.map((item) => item.broadcaster_id)).toEqual([
			"bc-2",
		]);
	});

	it("updates the flat list and single caches too", () => {
		const qc = new QueryClient();
		const caches = channelCaches(fakeTrpc());
		qc.setQueryData(
			[["channel", "list"]],
			[
				channel({
					broadcaster_id: "bc-1",
					user_state: { favorite: true, updated_at: "x" },
				}),
			],
		);
		qc.setQueryData(
			[["channel", "getById"], { input: { broadcaster_id: "bc-1" } }],
			channel({
				broadcaster_id: "bc-1",
				user_state: { favorite: true, updated_at: "x" },
			}),
		);

		patchEntity(qc, caches, channelFavoritePatch("bc-1", removedState));

		const list = qc.getQueryData<ChannelResponse[]>([["channel", "list"]]);
		expect(list?.[0]?.user_state?.favorite).toBe(false);
		const single = qc.getQueryData<ChannelResponse>([
			["channel", "getById"],
			{ input: { broadcaster_id: "bc-1" } },
		]);
		expect(single?.user_state?.favorite).toBe(false);
	});
});

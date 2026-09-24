// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
	CategoryResponse,
	ChannelResponse,
	VideoResponse,
} from "@/api/generated/trpc";
import { makeVideo } from "@/test/fixtures";

const navigateMock = vi.hoisted(() => vi.fn());
const searchState = vi.hoisted(() => ({
	videos: [] as VideoResponse[],
	channels: [] as ChannelResponse[],
	categories: [] as CategoryResponse[],
	isVideoFetching: false,
	isChannelFetching: false,
	isCategoryFetching: false,
}));
const categoryHooks = vi.hoisted(() => ({
	search: vi.fn(),
	searchWithVideos: vi.fn(),
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => navigateMock,
}));

vi.mock("@/hooks/useDebouncedValue", () => ({
	useDebouncedValue: <T,>(value: T) => value,
}));

vi.mock("@/features/videos", () => ({
	channelLabel: (video: VideoResponse) =>
		video.broadcaster_name || video.broadcaster_login || video.broadcaster_id,
	useVideoSearch: () => ({
		data: searchState.videos,
		isFetching: searchState.isVideoFetching,
	}),
}));

vi.mock("@/features/channels/queries", () => ({
	useChannelSearch: () => ({
		data: searchState.channels,
		isFetching: searchState.isChannelFetching,
	}),
}));

vi.mock("@/features/categories/queries", () => ({
	useCategorySearch: (...args: unknown[]) => {
		categoryHooks.search(...args);
		return {
			data: searchState.categories,
			isFetching: searchState.isCategoryFetching,
		};
	},
	useCategorySearchWithVideos: (...args: unknown[]) => {
		categoryHooks.searchWithVideos(...args);
		return {
			data: searchState.categories,
			isFetching: searchState.isCategoryFetching,
		};
	},
}));

import { GlobalSearch, GlobalSearchDialog } from "./global-search";

afterEach(() => {
	cleanup();
	navigateMock.mockClear();
	searchState.videos = [];
	searchState.channels = [];
	searchState.categories = [];
	searchState.isVideoFetching = false;
	searchState.isChannelFetching = false;
	searchState.isCategoryFetching = false;
	categoryHooks.search.mockClear();
	categoryHooks.searchWithVideos.mockClear();
});

describe("GlobalSearch", () => {
	it("renders grouped results and submits the first result", () => {
		searchState.videos = [
			video({ id: 42, title: "Neon Run", broadcaster_name: "Streamer" }),
		];
		searchState.channels = [channel({ broadcaster_name: "Streamer" })];
		searchState.categories = [category({ name: "Neon Game" })];

		render(createElement(GlobalSearch));
		const input = screen.getByRole("combobox", { name: "search.input_label" });
		fireEvent.change(input, { target: { value: "neon" } });

		expect(screen.getByText("search.group.videos")).toBeTruthy();
		expect(screen.getByText("search.group.channels")).toBeTruthy();
		expect(screen.getByText("search.group.categories")).toBeTruthy();
		expect(screen.getByText(byTextContent("Neon Run"))).toBeTruthy();
		expect(screen.getByText("videos.status.DONE")).toBeTruthy();

		fireEvent.submit(input.closest("form") as HTMLFormElement);

		expect(navigateMock).toHaveBeenCalledTimes(1);
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/dashboard/watch/$videoId",
			params: { videoId: "42" },
			search: { t: undefined },
		});
	});

	it("uses the library-only category search endpoint", () => {
		searchState.categories = [category({ name: "Neon Game" })];

		render(createElement(GlobalSearch));
		fireEvent.change(
			screen.getByRole("combobox", { name: "search.input_label" }),
			{
				target: { value: "neon" },
			},
		);

		expect(categoryHooks.search).not.toHaveBeenCalled();
		expect(categoryHooks.searchWithVideos).toHaveBeenCalledWith("neon", 3, {
			enabled: true,
		});
	});

	it("does not show stale disabled-scope data outside the active scope", () => {
		searchState.videos = [video({ title: "Neon Run" })];
		searchState.categories = [];

		render(createElement(GlobalSearch, { defaultScope: "categories" }));
		fireEvent.change(
			screen.getByRole("combobox", { name: "search.input_label" }),
			{
				target: { value: "neon" },
			},
		);

		expect(screen.queryByText("Neon Run")).toBeNull();
		expect(screen.getByText("search.no_results")).toBeTruthy();
	});

	it("navigates to channel results on click", () => {
		searchState.channels = [
			channel({ broadcaster_id: "bc-2", broadcaster_name: "Channel Two" }),
		];

		render(createElement(GlobalSearch));
		const input = screen.getByRole("combobox", {
			name: "search.input_label",
		}) as HTMLInputElement;
		fireEvent.change(input, { target: { value: "channel" } });
		fireEvent.click(screen.getByText(byTextContent("Channel Two")));

		expect(navigateMock).toHaveBeenCalledTimes(1);
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/dashboard/channels/$channelId",
			params: { channelId: "bc-2" },
		});
		expect(input.value).toBe("");
	});

	it("opens the mobile dialog search", () => {
		render(createElement(GlobalSearchDialog));

		fireEvent.click(screen.getByRole("button", { name: "search.open" }));

		expect(screen.getByRole("dialog")).toBeTruthy();
		expect(
			screen.getByRole("combobox", { name: "search.input_label" }),
		).toBeTruthy();
	});

	it("leaves the mobile dialog close to the backdrop and Escape", () => {
		render(createElement(GlobalSearchDialog));
		fireEvent.click(screen.getByRole("button", { name: "search.open" }));

		expect(screen.queryByRole("button", { name: "Close" })).toBeNull();

		fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });

		expect(screen.queryByRole("dialog")).toBeNull();
	});
});

function byTextContent(text: string) {
	return (_content: string, element: Element | null) =>
		element?.textContent === text &&
		Array.from(element.children).every((child) => child.textContent !== text);
}

function video(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return makeVideo(0, {
		thumbnail: undefined,
		primary_category_id: undefined,
		primary_category_name: undefined,
		...overrides,
	});
}

function channel(overrides: Partial<ChannelResponse> = {}): ChannelResponse {
	return {
		broadcaster_id: "bc-1",
		broadcaster_login: "streamer",
		broadcaster_name: "Streamer",
		view_count: 100,
		created_at: "2026-06-03T00:00:00Z",
		updated_at: "2026-06-03T00:00:00Z",
		...overrides,
	};
}

function category(overrides: Partial<CategoryResponse> = {}): CategoryResponse {
	return {
		id: "cat-1",
		name: "Game",
		created_at: "2026-06-03T00:00:00Z",
		updated_at: "2026-06-03T00:00:00Z",
		...overrides,
	};
}

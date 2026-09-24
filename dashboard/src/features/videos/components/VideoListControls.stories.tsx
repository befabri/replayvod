import { expect, fn, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeVideos } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import {
	VIDEO_LIST_SORT_KEYS,
	VIDEO_LIST_TABS,
	VIDEO_LIST_VIEWS,
	type VideoListFilters,
	type VideoListSortKey,
	type VideoListTab,
	type VideoListView,
} from "../list-search";
import {
	VideoListFilterChips,
	VideoScopeTabs,
	VideoSortSelect,
	VideoViewToggle,
} from "./VideoListControls";

const LANGUAGES = new Set(makeVideos(12).map((video) => video.language));

const TAB_COUNTS: Partial<Record<VideoListTab, number>> = Object.fromEntries(
	VIDEO_LIST_TABS.map((tab, index) => [tab, 1284 - index * 311]),
);

function Toolbar({
	sort,
	view,
	tab,
	filters,
	onSortChange,
	onViewChange,
	onTabChange,
	onFiltersChange,
}: {
	sort: VideoListSortKey;
	view: VideoListView;
	tab: VideoListTab;
	filters: VideoListFilters;
	onSortChange: (key: VideoListSortKey) => void;
	onViewChange: (view: VideoListView) => void;
	onTabChange: (tab: VideoListTab) => void;
	onFiltersChange: (patch: VideoListFilters) => void;
}) {
	return (
		<div className="space-y-6">
			<div className="flex flex-wrap items-center justify-end gap-2">
				<VideoViewToggle current={view} onChange={onViewChange} />
				<VideoSortSelect current={sort} onChange={onSortChange} />
			</div>
			<VideoScopeTabs
				current={tab}
				counts={TAB_COUNTS}
				onChange={onTabChange}
			/>
			<VideoListFilterChips
				filters={filters}
				languages={LANGUAGES}
				onChange={onFiltersChange}
			/>
		</div>
	);
}

const meta = preview.meta({
	title: "Features/Videos/VideoListControls",
	component: Toolbar,
	args: {
		sort: "newest" as const,
		view: "grid" as const,
		tab: "all" as const,
		filters: {},
		onSortChange: fn(),
		onViewChange: fn(),
		onTabChange: fn(),
		onFiltersChange: fn(),
	},
	argTypes: {
		sort: { control: "select", options: VIDEO_LIST_SORT_KEYS },
		view: { control: "inline-radio", options: VIDEO_LIST_VIEWS },
		tab: { control: "select", options: VIDEO_LIST_TABS },
	},
	parameters: { layout: "padded" },
	render: function Render(args, context) {
		const [sort, setSort] = useStoryArg(args, "sort", context);
		const [view, setView] = useStoryArg(args, "view", context);
		const [tab, setTab] = useStoryArg(args, "tab", context);
		const [filters, setFilters] = useStoryArg(args, "filters", context);
		return (
			<Toolbar
				sort={sort}
				view={view}
				tab={tab}
				filters={filters}
				onSortChange={(next) => {
					args.onSortChange(next);
					setSort(next);
				}}
				onViewChange={(next) => {
					args.onViewChange(next);
					setView(next);
				}}
				onTabChange={(next) => {
					args.onTabChange(next);
					setTab(next);
				}}
				onFiltersChange={(patch) => {
					args.onFiltersChange(patch);
					setFilters({ ...filters, ...patch });
				}}
			/>
		);
	},
});

export const Default = meta.story();

export const ActiveFilters = meta.story({
	args: {
		filters: {
			status: "FAILED",
			quality: "1080p60",
			language: "fr",
			duration: "long",
			source: "vod",
		},
	},
});

export const EverySortOption = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("combobox", { name: i18n.t("videos.sort_label") }),
		);
		await waitFor(() =>
			expect(
				screen.getByRole("listbox", { name: i18n.t("videos.sort_label") }),
			).toBeVisible(),
		);
		await expect(screen.getAllByRole("option")).toHaveLength(
			VIDEO_LIST_SORT_KEYS.length,
		);
	},
});

export const SwitchTab = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const watchLater = canvas.getByRole("tab", {
			name: new RegExp(i18n.t("videos.tabs.watch_later")),
		});
		await userEvent.click(watchLater);
		await expect(args.onTabChange).toHaveBeenCalledWith("watch_later");
		await expect(watchLater).toHaveAttribute("aria-selected", "true");
	},
});

export const ClearFilter = meta.story({
	args: { filters: { quality: "1080p60" } },
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("combobox", { name: i18n.t("videos.filter_quality") }),
		);
		await waitFor(() => expect(screen.getByRole("listbox")).toBeVisible());
		await userEvent.click(
			screen.getByRole("option", { name: i18n.t("videos.filter_any") }),
		);
		await expect(args.onFiltersChange).toHaveBeenCalledWith({
			quality: undefined,
		});
	},
});

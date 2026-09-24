import { SortAscendingIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { VirtualGrid, VirtualGridSkeleton } from "@/components/ui/virtual-grid";
import { type ChannelResponse, useInfiniteChannels } from "@/features/channels";
import { ChannelFavoriteButton } from "@/features/channels/components/ChannelFavoriteButton";
import { useLiveSet } from "@/features/streams-live";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";
import { cn } from "@/lib/utils";

const SORT_MODES = ["name_asc", "name_desc"] as const;
type SortMode = (typeof SORT_MODES)[number];

const FILTER_MODES = ["all", "live", "downloaded", "favorites"] as const;
type FilterMode = (typeof FILTER_MODES)[number];

const CHANNEL_GRID = { minItemWidth: 220, gap: 8 } as const;
const CHANNEL_TILE_CLASS =
	"flex items-center gap-2 rounded-md bg-card px-2 py-2 shadow-sm";
const LOADING_TILES = 24;

export const Route = createFileRoute("/dashboard/channels")({
	validateSearch: (search: Record<string, unknown>) => ({
		sort: SORT_MODES.includes(search.sort as SortMode)
			? (search.sort as SortMode)
			: ("name_asc" as SortMode),
		filter: FILTER_MODES.includes(search.filter as FilterMode)
			? (search.filter as FilterMode)
			: ("all" as FilterMode),
	}),
	component: ChannelsPage,
});

function ChannelsPage() {
	const { t } = useTranslation();
	const { sort, filter } = Route.useSearch();
	const navigate = Route.useNavigate();
	const channelListFilter =
		filter === "downloaded" || filter === "favorites" ? filter : "all";
	const channels = useInfiniteChannels(sort, channelListFilter);
	const liveSet = useLiveSet();
	const resource = useInfiniteResource(channels, {
		getItems: (page) => page.items,
		rootMargin: "500px 0px",
		shouldLoadMore: ({ query }) =>
			!!query.hasNextPage &&
			!query.error &&
			(filter !== "live" || liveSet.size > 0),
	});
	const visible = useMemo(
		() =>
			filter === "live"
				? resource.items.filter((c) => liveSet.has(c.broadcaster_id))
				: resource.items,
		[resource.items, filter, liveSet],
	);
	const hasScrolledThroughPages = resource.hasScrolledThroughPages;
	const shouldLoadMore = resource.shouldLoadMore;
	const loadMoreRef = resource.loadMoreRef;
	const showEmpty = !!(
		visible.length === 0 &&
		!channels.isLoading &&
		!channels.isFetchingNextPage &&
		!channels.error &&
		(filter !== "live" || liveSet.size === 0 || !channels.hasNextPage)
	);
	const showSearchingMore = !!(
		visible.length === 0 &&
		!channels.isLoading &&
		!channels.error &&
		filter === "live" &&
		liveSet.size > 0 &&
		(channels.isFetchingNextPage || channels.hasNextPage)
	);
	const emptyMessage =
		filter === "live"
			? t("channels.empty_live")
			: filter === "downloaded"
				? t("channels.empty_downloaded")
				: filter === "favorites"
					? t("channels.empty_favorites")
					: t("channels.empty");
	const getChannelKey = useCallback(
		(channel: ChannelResponse) => channel.broadcaster_id,
		[],
	);
	const renderChannel = useCallback(
		(channel: ChannelResponse) => (
			<div
				className={cn(
					CHANNEL_TILE_CLASS,
					"transition-colors duration-75 hover:bg-accent hover:text-accent-foreground",
				)}
			>
				<Link
					to="/dashboard/channels/$channelId"
					params={{ channelId: channel.broadcaster_id }}
					className="flex min-w-0 flex-1 items-center gap-3 rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
				>
					<Avatar
						src={channel.profile_image_url}
						name={channel.broadcaster_name}
						alt={channel.broadcaster_name}
						size="md"
						isLive={liveSet.has(channel.broadcaster_id)}
					/>
					<span className="truncate text-sm font-medium">
						{channel.broadcaster_name}
					</span>
				</Link>
				<ChannelFavoriteButton
					broadcasterId={channel.broadcaster_id}
					favorite={channel.user_state?.favorite ?? false}
				/>
			</div>
		),
		[liveSet],
	);
	return (
		<TitledLayout
			title={t("nav.channels")}
			actions={
				<SortDropdown
					current={sort}
					onChange={(next) =>
						void navigate({ search: (s) => ({ ...s, sort: next }) })
					}
				/>
			}
		>
			<div className="space-y-6">
				<ChannelTabs
					current={filter}
					onChange={(next) => {
						void navigate({ search: (s) => ({ ...s, filter: next }) });
					}}
				/>

				{channels.isLoading && (
					<VirtualGridSkeleton
						count={LOADING_TILES}
						renderItem={() => <ChannelTileSkeleton />}
						{...CHANNEL_GRID}
					/>
				)}

				{channels.error && (
					<Alert variant="destructive">
						{t("channels.failed_to_load")}: {channels.error.message}
					</Alert>
				)}

				{showEmpty && <EmptyPanel>{emptyMessage}</EmptyPanel>}
				{showSearchingMore && <LoadingState />}

				{visible.length > 0 && (
					<VirtualGrid
						items={visible}
						getItemKey={getChannelKey}
						renderItem={renderChannel}
						estimateRowHeight={48}
						overscan={8}
						{...CHANNEL_GRID}
					/>
				)}
				{shouldLoadMore && <div ref={loadMoreRef} className="h-1" />}
				{hasScrolledThroughPages &&
					!channels.hasNextPage &&
					!channels.isFetchingNextPage &&
					visible.length > 0 && (
						<VideoGridEnd labelKey="channels.end_of_list" />
					)}
			</div>
		</TitledLayout>
	);
}

function ChannelTileSkeleton() {
	return (
		<div className={cn(CHANNEL_TILE_CLASS, "gap-3")}>
			<Skeleton className="size-8 shrink-0 rounded-full" />
			<Skeleton className="h-4 w-2/3" />
		</div>
	);
}

function ChannelTabs({
	current,
	onChange,
}: {
	current: FilterMode;
	onChange: (m: FilterMode) => void;
}) {
	const { t } = useTranslation();
	return (
		<FilterTabs
			value={current}
			onChange={(value) => onChange(value as FilterMode)}
			options={FILTER_MODES.map((mode) => ({
				value: mode,
				label: t(`channels.tabs.${mode}`),
			}))}
		/>
	);
}

function SortDropdown({
	current,
	onChange,
}: {
	current: SortMode;
	onChange: (m: SortMode) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<SortMode, string> = {
		name_asc: t("channels.sort_asc"),
		name_desc: t("channels.sort_desc"),
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<SortAscendingIcon className="size-4" />
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{SORT_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{labels[mode]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

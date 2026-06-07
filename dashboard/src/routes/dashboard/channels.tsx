import { SortAscendingIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
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
import { VirtualGrid } from "@/components/ui/virtual-grid";
import { type ChannelResponse, useInfiniteChannels } from "@/features/channels";
import { ChannelFavoriteButton } from "@/features/channels/components/ChannelFavoriteButton";
import { useLiveSet } from "@/features/streams-live";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { useInfiniteScrollSentinel } from "@/hooks/useInfiniteScrollSentinel";

const SORT_MODES = ["name_asc", "name_desc"] as const;
type SortMode = (typeof SORT_MODES)[number];

const FILTER_MODES = ["all", "live", "downloaded", "favorites"] as const;
type FilterMode = (typeof FILTER_MODES)[number];

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
	// filter === "live" is applied client-side against the SSE-
	// backed liveSet so channels going on- or offline reflect in
	// real time. The downloaded tab is API-backed because it depends
	// on local video rows.
	const visible = useMemo(() => {
		const all = channels.data?.pages.flatMap((page) => page.items) ?? [];
		return filter === "live"
			? all.filter((c) => liveSet.has(c.broadcaster_id))
			: all;
	}, [channels.data, filter, liveSet]);
	const hasScrolledThroughPages = (channels.data?.pages.length ?? 0) > 1;
	// Avoid draining every channel page when the live filter is active
	// but the SSE live set says nobody is live.
	const shouldLoadMore = !!(
		channels.hasNextPage &&
		!channels.error &&
		(filter !== "live" || liveSet.size > 0)
	);
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
			<div className="flex items-center gap-2 rounded-md bg-card px-2 py-2 shadow-sm transition-colors duration-75 hover:bg-accent hover:text-accent-foreground">
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
	const loadMoreRef = useInfiniteScrollSentinel({
		enabled: shouldLoadMore,
		isLoadingMore: channels.isFetchingNextPage,
		onLoadMore: () => channels.fetchNextPage(),
		rootMargin: "500px 0px",
	});

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
					<div className="text-muted-foreground">{t("common.loading")}</div>
				)}

				{channels.error && (
					<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
						{t("channels.failed_to_load")}: {channels.error.message}
					</div>
				)}

				{showEmpty && <EmptyPanel>{emptyMessage}</EmptyPanel>}
				{showSearchingMore && (
					<div className="text-muted-foreground">{t("common.loading")}</div>
				)}

				{visible.length > 0 && (
					<VirtualGrid
						items={visible}
						getItemKey={getChannelKey}
						renderItem={renderChannel}
						minItemWidth={220}
						estimateRowHeight={48}
						gap={8}
						overscan={8}
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

import { FunnelSimpleIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryBoundary } from "@/components/query-boundary";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { usePlaybackSettings } from "@/features/settings/playback";
import { useInfiniteVideoPages, useStatistics } from "@/features/videos";
import { videoListColumns } from "@/features/videos/components/listColumns";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import {
	VideoListFilterChips,
	VideoScopeTabs,
	VideoSortSelect,
	VideoViewToggle,
} from "@/features/videos/components/VideoListControls";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { formatBytes } from "@/features/videos/format";
import {
	isOneOf,
	VIDEO_DURATION_FILTERS,
	VIDEO_LIST_SORT_CONFIG,
	VIDEO_LIST_SORT_KEYS,
	VIDEO_LIST_STATUSES,
	VIDEO_LIST_TABS,
	VIDEO_LIST_VIEWS,
	VIDEO_SOURCE_FILTERS,
	type VideoDurationFilter,
	type VideoListFilters,
	type VideoListTab,
} from "@/features/videos/list-search";
import { useCanManageVideos } from "@/features/videos/permissions";
import {
	isContinueWatchingVideo,
	type ResumePolicy,
} from "@/features/videos/resume-policy";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 50;

const THIS_WEEK_MS = 7 * 24 * 60 * 60 * 1000;

function parseStringParam(raw: unknown): string | undefined {
	return typeof raw === "string" && raw.length > 0 ? raw : undefined;
}

export function validateVideosSearch(search: Record<string, unknown>) {
	return {
		tab:
			search.tab === "favorites"
				? "watch_later"
				: isOneOf(VIDEO_LIST_TABS, search.tab)
					? search.tab
					: "all",
		status: isOneOf(VIDEO_LIST_STATUSES, search.status)
			? search.status
			: undefined,
		view: isOneOf(VIDEO_LIST_VIEWS, search.view) ? search.view : "grid",
		sort: isOneOf(VIDEO_LIST_SORT_KEYS, search.sort)
			? search.sort
			: search.tab === "continue_watching"
				? "recently_watched"
				: "newest",
		quality: parseStringParam(search.quality),
		language: parseStringParam(search.language),
		duration: isOneOf(VIDEO_DURATION_FILTERS, search.duration)
			? search.duration
			: undefined,
		source: isOneOf(VIDEO_SOURCE_FILTERS, search.source)
			? search.source
			: undefined,
	};
}

export type VideosSearch = ReturnType<typeof validateVideosSearch>;

export function videosSearchForTabChange(
	search: VideosSearch,
	tab: VideoListTab,
): VideosSearch {
	return {
		...search,
		tab,
		sort:
			tab === "continue_watching"
				? "recently_watched"
				: search.sort === "recently_watched"
					? "newest"
					: search.sort,
		status: undefined,
		quality: undefined,
		language: undefined,
		duration: undefined,
		source: undefined,
	};
}

export const Route = createFileRoute("/dashboard/videos")({
	validateSearch: validateVideosSearch,
	component: VideosPage,
});

function VideosPage() {
	return (
		<QueryBoundary fallback={<VideosPageLoading />}>
			<VideosLibrary />
		</QueryBoundary>
	);
}

function VideosPageLoading() {
	const { t } = useTranslation();
	return (
		<TitledLayout
			title={t("videos.title")}
			description={<VideosSummarySkeleton />}
		>
			<div className="space-y-6">
				<Skeleton className="h-9 w-full max-w-xl" />
				<VideoGridLoading className="mt-0" />
			</div>
		</TitledLayout>
	);
}

function VideosSummarySkeleton() {
	return (
		<span className="flex h-5 items-center">
			<Skeleton className="h-3.5 w-72 max-w-full" />
		</span>
	);
}

function VideosLibrary() {
	const policy = usePlaybackSettings();
	const { t, i18n } = useTranslation();
	const {
		tab,
		status,
		view,
		sort: sortKey,
		quality,
		language,
		duration,
		source,
	} = Route.useSearch();
	const navigate = Route.useNavigate();
	const [filtersOpen, setFiltersOpen] = useState(false);
	const { data: stats, isPending: statsPending } = useStatistics();
	const canManage = useCanManageVideos();

	const sortConfig = VIDEO_LIST_SORT_CONFIG[sortKey];

	const videos = useInfiniteVideoPages(
		PAGE_SIZE,
		status,
		sortConfig.sort,
		sortConfig.order,
		{
			quality,
			language,
			duration,
			source,
			window: tab === "this_week" ? "this_week" : undefined,
			watchLaterOnly: tab === "watch_later",
			unwatchedOnly: tab === "unwatched",
			continueWatchingOnly: tab === "continue_watching",
		},
	);
	const resource = useInfiniteResource(videos, {
		getItems: (page) => page.items,
		rootMargin: "500px 0px",
	});
	const loadedRows = resource.items;
	const hasScrolledThroughPages = resource.hasScrolledThroughPages;
	const shouldLoadMore = resource.shouldLoadMore;
	const loadMoreRef = resource.loadMoreRef;

	const tabCounts: Partial<Record<VideoListTab, number>> = {
		all: stats?.total,
		this_week: stats?.this_week,
		unwatched: stats?.unwatched,
		watch_later: stats?.watch_later,
		continue_watching: stats?.continue_watching,
	};
	const seenLanguages = useLanguageFacet(loadedRows, language);

	const columns = useMemo(
		() => videoListColumns(t, canManage, i18n.language),
		[t, canManage, i18n.language],
	);
	const filteredVideos = useMemo(
		() =>
			filterLoadedVideosForSearch(
				loadedRows,
				{
					tab,
					status,
					quality: videos.isPlaceholderData ? quality : undefined,
					language,
					duration,
					source,
				},
				policy,
			),
		[
			loadedRows,
			tab,
			status,
			quality,
			language,
			duration,
			source,
			policy,
			videos.isPlaceholderData,
		],
	);
	const hasActiveFilters = !!(
		status ||
		quality ||
		language ||
		duration ||
		source
	);
	const showingLabel = t("videos.showing_loaded", {
		shown: filteredVideos.length,
		loaded: loadedRows.length,
	});
	const showEmpty =
		loadedRows.length === 0 && !videos.isLoading && !videos.error;
	const showNoMatchYet =
		loadedRows.length > 0 &&
		filteredVideos.length === 0 &&
		!videos.isLoading &&
		!videos.error;
	const emptyMessage = hasActiveFilters
		? t("videos.no_match")
		: tab === "watch_later"
			? t("videos.empty_watch_later")
			: tab === "unwatched"
				? t("videos.empty_unwatched")
				: tab === "continue_watching"
					? t("videos.empty_continue_watching")
					: t("videos.empty");
	const summary = stats
		? t("videos.summary", {
				count: stats.total.toLocaleString(),
				size: formatBytes(stats.total_size),
				channels: stats.channels.toLocaleString(),
			})
		: undefined;

	const setFilter = (patch: VideoListFilters) => {
		void navigate({ search: (s) => ({ ...s, ...patch }) });
	};

	return (
		<TitledLayout
			title={t("videos.title")}
			description={statsPending ? <VideosSummarySkeleton /> : summary}
			actions={
				<>
					<VideoViewToggle
						current={view}
						onChange={(next) => {
							void navigate({ search: (s) => ({ ...s, view: next }) });
						}}
					/>
					<VideoSortSelect
						current={sortKey}
						onChange={(next) => {
							void navigate({ search: (s) => ({ ...s, sort: next }) });
						}}
					/>
					<Button
						variant="ghost"
						onClick={() => setFiltersOpen((open) => !open)}
						className={cn(
							"bg-card focus-visible:border-transparent focus-visible:ring-0",
							(filtersOpen || hasActiveFilters) &&
								"bg-primary text-primary-foreground hover:bg-primary-hover hover:text-primary-foreground",
						)}
					>
						<FunnelSimpleIcon className="size-4" />
						{t("videos.filters")}
					</Button>
				</>
			}
		>
			<div className="space-y-6">
				<VideoScopeTabs
					current={tab}
					counts={tabCounts}
					onChange={(next) => {
						void navigate({
							search: (s) => videosSearchForTabChange(s, next),
						});
					}}
				/>

				{filtersOpen ? (
					<div className="flex animate-in flex-col gap-3 fade-in-0 slide-in-from-top-1 duration-150 sm:flex-row sm:items-center sm:justify-between">
						<VideoListFilterChips
							filters={{ status, quality, language, duration, source }}
							languages={seenLanguages}
							onChange={setFilter}
						/>
						<div className="text-xs tracking-[0.12em] text-muted-foreground uppercase">
							{showingLabel}
						</div>
					</div>
				) : null}

				{videos.isLoading &&
					(view === "grid" ? (
						<VideoGridLoading className="mt-0" />
					) : (
						<DataTable columns={columns} data={[]} loading />
					))}

				{videos.error && (
					<Alert variant="destructive">
						{t("videos.failed_to_load")}: {videos.error.message}
					</Alert>
				)}

				{showEmpty && <EmptyPanel>{emptyMessage}</EmptyPanel>}

				{showNoMatchYet && (
					<EmptyPanel>
						{videos.hasNextPage || videos.isFetchingNextPage
							? t("videos.no_match_loaded")
							: t("videos.no_match")}
					</EmptyPanel>
				)}

				{filteredVideos.length > 0 &&
					(view === "grid" ? (
						<VirtualVideoGrid videos={filteredVideos} canManage={canManage} />
					) : (
						<DataTable
							columns={columns}
							data={filteredVideos}
							emptyMessage={t("videos.empty")}
							virtualizeRows
							estimateRowHeight={84}
						/>
					))}

				{shouldLoadMore && <div ref={loadMoreRef} className="h-1" />}

				{videos.isFetchingNextPage &&
					(view === "grid" ? (
						<VideoGridLoading count={3} />
					) : (
						<LoadingState className="rounded-xl border border-border bg-card/70 p-4" />
					))}

				{hasScrolledThroughPages &&
					!videos.hasNextPage &&
					!videos.isFetchingNextPage && <VideoGridEnd />}
			</div>
		</TitledLayout>
	);
}

function useLanguageFacet(
	rows: VideoResponse[],
	currentValue: string | undefined,
) {
	const [seen, setSeen] = useState<Set<string>>(() =>
		currentValue ? new Set([currentValue]) : new Set(),
	);
	useEffect(() => {
		setSeen((prev) => {
			let changed = false;
			const next = new Set(prev);
			if (currentValue && !next.has(currentValue)) {
				next.add(currentValue);
				changed = true;
			}
			for (const row of rows) {
				if (row.language && !next.has(row.language)) {
					next.add(row.language);
					changed = true;
				}
			}
			return changed ? next : prev;
		});
	}, [rows, currentValue]);
	return seen;
}

export function filterLoadedVideosForSearch(
	rows: VideoResponse[],
	search: Pick<
		VideosSearch,
		"tab" | "status" | "quality" | "language" | "duration" | "source"
	>,
	policy: ResumePolicy,
	nowMs = Date.now(),
) {
	return rows.filter((video) => {
		if (search.status && video.status !== search.status) return false;
		if (search.source && video.source !== search.source) return false;
		if (search.quality && video.quality !== search.quality) return false;
		if (search.language && video.language !== search.language) return false;
		if (!matchesDurationFilter(video.duration_seconds, search.duration)) {
			return false;
		}
		return matchesTabFilter(video, search.tab, nowMs, policy);
	});
}

function matchesTabFilter(
	video: VideoResponse,
	tab: VideoListTab,
	nowMs: number,
	policy: ResumePolicy,
) {
	if (tab === "continue_watching")
		return isContinueWatchingVideo(video, policy);
	if (tab === "watch_later") return video.user_state?.watch_later === true;
	if (tab === "unwatched") {
		return video.status === "DONE" && !video.user_state?.watched_at;
	}
	if (tab === "this_week") {
		const startedAt = Date.parse(video.start_download_at);
		return Number.isFinite(startedAt) && startedAt >= nowMs - THIS_WEEK_MS;
	}
	return true;
}

function matchesDurationFilter(
	seconds: number | undefined,
	filter: VideoDurationFilter | undefined,
) {
	if (!filter) return true;
	if (!seconds || seconds <= 0) return false;
	if (filter === "short") return seconds < 30 * 60;
	if (filter === "medium") return seconds >= 30 * 60 && seconds < 2 * 3600;
	if (filter === "long") return seconds >= 2 * 3600 && seconds < 4 * 3600;
	return seconds >= 4 * 3600;
}

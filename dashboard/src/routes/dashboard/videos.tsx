import {
	FunnelSimpleIcon,
	RowsIcon,
	SortAscendingIcon,
	SquaresFourIcon,
} from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { VideoResponse, VideoStatus } from "@/api/generated/trpc";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { FilterTabs } from "@/components/ui/filter-tabs";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
} from "@/components/ui/select";
import {
	useInfiniteVideoPages,
	useStatistics,
	type VideoOrder,
	type VideoSort,
} from "@/features/videos";
import { videoListColumns } from "@/features/videos/components/listColumns";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { formatBytes } from "@/features/videos/format";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 50;

type ViewMode = "grid" | "table";
type SortKey =
	| "newest"
	| "oldest"
	| "channel_asc"
	| "channel_desc"
	| "longest"
	| "largest";

const SORT_CONFIG: Record<SortKey, { sort: VideoSort; order: VideoOrder }> = {
	newest: { sort: "created_at", order: "desc" },
	oldest: { sort: "created_at", order: "asc" },
	channel_asc: { sort: "channel", order: "asc" },
	channel_desc: { sort: "channel", order: "desc" },
	longest: { sort: "duration", order: "desc" },
	largest: { sort: "size", order: "desc" },
};

const SORT_KEYS = Object.keys(SORT_CONFIG) as SortKey[];
const STATUS_KEYS = [
	"DONE",
	"RUNNING",
	"PENDING",
	"FAILED",
] as const satisfies readonly VideoStatus[];
type StatusKey = (typeof STATUS_KEYS)[number];

const TAB_KEYS = ["all", "this_week", "unwatched", "watch_later"] as const;
type TabKey = (typeof TAB_KEYS)[number];

const DURATION_FILTERS = ["short", "medium", "long", "marathon"] as const;
type DurationFilter = (typeof DURATION_FILTERS)[number];
const THIS_WEEK_MS = 7 * 24 * 60 * 60 * 1000;

// Sentinel for the "no filter" option in the Select widget. Base UI
// Select treats each non-empty string value as a distinct option;
// using an empty string risks colliding with unset-value semantics
// in the primitive, so we round-trip through "any" on the UI side
// and undefined on the URL/server side.
const ANY = "any";

// Twitch's quality ladder — a small closed set so the quality
// dropdown renders the full range even when the current filter
// narrows data rows down to a single rendition. Sourced from the
// labels the variant picker emits plus Helix's fallback enums.
const QUALITY_LADDER = [
	"1080p60",
	"1080p",
	"720p60",
	"720p",
	"480p",
	"360p",
	"160p",
	"chunked",
	"audio_only",
] as const;

function isOneOf<T extends string>(
	values: readonly T[],
	raw: unknown,
): raw is T {
	return typeof raw === "string" && values.includes(raw as T);
}

function parseStringParam(raw: unknown): string | undefined {
	return typeof raw === "string" && raw.length > 0 ? raw : undefined;
}

export function validateVideosSearch(search: Record<string, unknown>) {
	return {
		tab:
			search.tab === "favorites"
				? "watch_later"
				: isOneOf(TAB_KEYS, search.tab)
					? search.tab
					: "all",
		status: isOneOf(STATUS_KEYS, search.status) ? search.status : undefined,
		view:
			search.view === "table" || search.view === "grid"
				? (search.view as ViewMode)
				: "grid",
		sort: isOneOf(SORT_KEYS, search.sort) ? search.sort : "newest",
		quality: parseStringParam(search.quality),
		language: parseStringParam(search.language),
		duration: isOneOf(DURATION_FILTERS, search.duration)
			? search.duration
			: undefined,
	};
}

export type VideosSearch = ReturnType<typeof validateVideosSearch>;

export function videosSearchForTabChange(
	search: VideosSearch,
	tab: TabKey,
): VideosSearch {
	return {
		...search,
		tab,
		status: undefined,
		quality: undefined,
		language: undefined,
		duration: undefined,
	};
}

export const Route = createFileRoute("/dashboard/videos")({
	validateSearch: validateVideosSearch,
	component: VideosPage,
});

function VideosPage() {
	const { t, i18n } = useTranslation();
	const {
		tab,
		status,
		view,
		sort: sortKey,
		quality,
		language,
		duration,
	} = Route.useSearch();
	const navigate = Route.useNavigate();
	const [filtersOpen, setFiltersOpen] = useState(false);
	const { data: stats } = useStatistics();
	// Resolve the delete permission once for the whole page; the table columns
	// and the grid cards omit the remove control for viewers rather than each
	// row subscribing to the auth store and rendering null.
	const canManage = useCanManageVideos();

	const sortConfig = SORT_CONFIG[sortKey];

	const videos = useInfiniteVideoPages(
		PAGE_SIZE,
		status,
		sortConfig.sort,
		sortConfig.order,
		{
			quality,
			language,
			duration,
			window: tab === "this_week" ? "this_week" : undefined,
			watchLaterOnly: tab === "watch_later",
			unwatchedOnly: tab === "unwatched",
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

	const tabCounts: Partial<Record<TabKey, number>> = {
		all: stats?.total,
		this_week: stats?.this_week,
		unwatched: stats?.unwatched,
		watch_later: stats?.watch_later,
	};
	// Languages grow across the session so the dropdown doesn't
	// collapse to a single option once the user narrows the server
	// filter. Reset on page reload. See note in useLanguageFacet.
	const seenLanguages = useLanguageFacet(loadedRows, language);

	const columns = useMemo(
		() => videoListColumns(t, canManage, i18n.language),
		[t, canManage, i18n.language],
	);
	const statusOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY, label: t("videos.status_filter.any") },
					...STATUS_KEYS.map((key) => ({
						value: key,
						label: t(`videos.status_filter.${key}` as const),
					})),
				],
				status,
			),
		[status, t],
	);
	const qualityOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY, label: t("videos.filter_any") },
					...QUALITY_LADDER.map((q) => ({ value: q, label: q })),
				],
				quality,
			),
		[quality, t],
	);
	const languageOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY, label: t("videos.filter_any") },
					...[...seenLanguages]
						.sort((a, b) => a.localeCompare(b))
						.map((value) => ({ value, label: value.toUpperCase() })),
				],
				language,
			),
		[seenLanguages, language, t],
	);
	const durationOptions = useMemo(
		() => [
			{ value: ANY, label: t("videos.duration_any") },
			{ value: "short", label: t("videos.duration_short") },
			{ value: "medium", label: t("videos.duration_medium") },
			{ value: "long", label: t("videos.duration_long") },
			{ value: "marathon", label: t("videos.duration_marathon") },
		],
		[t],
	);
	// Narrow previous-query rows while placeholderData keeps them mounted.
	const filteredVideos = useMemo(
		() =>
			filterLoadedVideosForSearch(loadedRows, {
				tab,
				status,
				quality,
				language,
				duration,
			}),
		[loadedRows, tab, status, quality, language, duration],
	);
	const hasActiveFilters = !!(status || quality || language || duration);
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
				: t("videos.empty");
	const summary = stats
		? t("videos.summary", {
				count: stats.total.toLocaleString(),
				size: formatBytes(stats.total_size),
				channels: stats.channels.toLocaleString(),
			})
		: undefined;

	const setFilter = (patch: {
		status?: StatusKey;
		quality?: string;
		language?: string;
		duration?: DurationFilter;
	}) => {
		void navigate({ search: (s) => ({ ...s, ...patch }) });
	};

	return (
		<TitledLayout
			title={t("videos.title")}
			description={summary}
			actions={
				<>
					<ViewToggle
						current={view}
						onChange={(next) => {
							void navigate({ search: (s) => ({ ...s, view: next }) });
						}}
					/>
					<SortSelect
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
				<ScopeTabs
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
						<div className="flex flex-wrap items-center gap-2.5">
							<FilterChipSelect
								label={t("videos.filter_status")}
								value={status ?? ANY}
								options={statusOptions}
								onChange={(value) =>
									setFilter({
										status: isOneOf(STATUS_KEYS, value) ? value : undefined,
									})
								}
							/>
							<FilterChipSelect
								label={t("videos.filter_quality")}
								value={quality ?? ANY}
								options={qualityOptions}
								onChange={(value) =>
									setFilter({ quality: value === ANY ? undefined : value })
								}
							/>
							<FilterChipSelect
								label={t("videos.filter_language")}
								value={language ?? ANY}
								options={languageOptions}
								onChange={(value) =>
									setFilter({ language: value === ANY ? undefined : value })
								}
							/>
							<FilterChipSelect
								label={t("videos.filter_duration")}
								value={duration ?? ANY}
								options={durationOptions}
								onChange={(value) =>
									setFilter({
										duration: isOneOf(DURATION_FILTERS, value)
											? value
											: undefined,
									})
								}
							/>
						</div>
						<div className="text-xs tracking-[0.12em] text-muted-foreground uppercase">
							{showingLabel}
						</div>
					</div>
				) : null}

				{videos.isLoading &&
					(view === "grid" ? (
						<VideoGridLoading className="mt-0" />
					) : (
						<div className="text-muted-foreground">{t("common.loading")}</div>
					))}

				{videos.error && (
					<div className="rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive shadow-sm">
						{t("videos.failed_to_load")}: {videos.error.message}
					</div>
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
						<div className="rounded-xl border border-border bg-card/70 p-4 text-sm text-muted-foreground">
							{t("common.loading")}
						</div>
					))}

				{hasScrolledThroughPages &&
					!videos.hasNextPage &&
					!videos.isFetchingNextPage && <VideoGridEnd />}
			</div>
		</TitledLayout>
	);
}

// useLanguageFacet accumulates every distinct language code seen in
// loaded rows across the session. The language dropdown renders this
// set so it never collapses to a single option once the user applies
// a narrowing server filter. Reset on full page reload. Also seeds
// from the URL-driven `currentValue` so a user who lands on a
// narrow ?language=... sees their own selection in the dropdown even
// before any unfiltered rows have been loaded.
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

function SortSelect({
	current,
	onChange,
}: {
	current: SortKey;
	onChange: (key: SortKey) => void;
}) {
	const { t } = useTranslation();
	const label = t(`videos.sort.${current}`);
	return (
		<Select value={current} onValueChange={(next) => onChange(next as SortKey)}>
			<SelectTrigger variant="chip" className="min-w-[150px]">
				<div className="flex items-center gap-2">
					<SortAscendingIcon className="size-4 text-muted-foreground" />
					<span className="truncate text-sm font-medium">{label}</span>
				</div>
			</SelectTrigger>
			<SelectContent>
				{SORT_KEYS.map((key) => (
					<SelectItem key={key} value={key}>
						{t(`videos.sort.${key}`)}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

function ViewToggle({
	current,
	onChange,
}: {
	current: ViewMode;
	onChange: (mode: ViewMode) => void;
}) {
	const { t } = useTranslation();
	const modes: Array<{ key: ViewMode; icon: React.ReactNode }> = [
		{ key: "grid", icon: <SquaresFourIcon className="size-4" /> },
		{ key: "table", icon: <RowsIcon className="size-4" /> },
	];
	return (
		<fieldset className="inline-flex items-center rounded-md border border-border bg-card p-0.5">
			<legend className="sr-only">{t("videos.view_label")}</legend>
			{modes.map((mode) => {
				const active = current === mode.key;
				return (
					<Button
						key={mode.key}
						variant="ghost"
						size="sm"
						aria-pressed={active}
						onClick={() => onChange(mode.key)}
						className={cn(
							"h-8 rounded-md px-3 text-xs text-muted-foreground",
							active &&
								"bg-primary text-primary-foreground hover:bg-primary-hover hover:text-primary-foreground",
						)}
					>
						{mode.icon}
						{t(`videos.view.${mode.key}`)}
					</Button>
				);
			})}
		</fieldset>
	);
}

function ScopeTabs({
	current,
	counts,
	onChange,
}: {
	current: TabKey;
	// counts is keyed by tab. Values come from the statistics endpoint
	// and are exact for tabs that have server backing.
	counts: Partial<Record<TabKey, number>>;
	onChange: (key: TabKey) => void;
}) {
	const { t } = useTranslation();
	return (
		<FilterTabs
			value={current}
			onChange={(value) => onChange(value as TabKey)}
			options={TAB_KEYS.map((key) => ({
				value: key,
				label: t(`videos.tabs.${key}`),
				count: counts[key],
			}))}
		/>
	);
}

function FilterChipSelect({
	label,
	value,
	options,
	onChange,
}: {
	label: string;
	value: string;
	options: Array<{ value: string; label: string }>;
	onChange: (value: string) => void;
}) {
	const selected =
		options.find((option) => option.value === value) ?? options[0];
	return (
		<Select value={value} onValueChange={(next) => onChange(String(next))}>
			<SelectTrigger variant="chip" className="min-w-[138px]">
				<span className="truncate text-sm">
					<span className="text-muted-foreground">{label}:</span>{" "}
					<span className="font-medium text-foreground">{selected?.label}</span>
				</span>
			</SelectTrigger>
			<SelectContent>
				{options.map((option) => (
					<SelectItem key={option.value} value={option.value}>
						{option.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

export function filterLoadedVideosForSearch(
	rows: VideoResponse[],
	search: Pick<
		VideosSearch,
		"tab" | "status" | "quality" | "language" | "duration"
	>,
	nowMs = Date.now(),
) {
	return rows.filter((video) => {
		if (search.status && video.status !== search.status) return false;
		if (search.quality && video.quality !== search.quality) return false;
		if (search.language && video.language !== search.language) return false;
		if (!matchesDurationFilter(video.duration_seconds, search.duration)) {
			return false;
		}
		return matchesTabFilter(video, search.tab, nowMs);
	});
}

function matchesTabFilter(video: VideoResponse, tab: TabKey, nowMs: number) {
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

// withSelectedOption makes sure the URL-held filter value has a
// matching entry in the dropdown options, synthesising one if the
// derived list (ladder, useChannels, seenLanguages) doesn't cover
// it yet. Without this, the trigger label would silently fall back
// to "any" while the URL and server filter still reflect the real
// value.
function withSelectedOption(
	options: Array<{ value: string; label: string }>,
	selected: string | undefined,
) {
	if (!selected || options.some((o) => o.value === selected)) return options;
	return [...options, { value: selected, label: selected }];
}

function matchesDurationFilter(
	seconds: number | undefined,
	filter: DurationFilter | undefined,
) {
	if (!filter) return true;
	if (!seconds || seconds <= 0) return false;
	if (filter === "short") return seconds < 30 * 60;
	if (filter === "medium") return seconds >= 30 * 60 && seconds < 2 * 3600;
	if (filter === "long") return seconds >= 2 * 3600 && seconds < 4 * 3600;
	return seconds >= 4 * 3600;
}

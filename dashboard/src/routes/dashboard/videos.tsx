import {
	FunnelSimple,
	Rows,
	SortAscending,
	SquaresFour,
} from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useChannels } from "@/features/channels";
import {
	channelLabel,
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
const STATUS_KEYS = ["all", "DONE", "RUNNING", "PENDING", "FAILED"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

const DURATION_FILTERS = ["short", "medium", "long", "marathon"] as const;
type DurationFilter = (typeof DURATION_FILTERS)[number];
const SIZE_FILTERS = ["small", "medium", "large"] as const;
type SizeFilter = (typeof SIZE_FILTERS)[number];

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

export const Route = createFileRoute("/dashboard/videos")({
	validateSearch: (search: Record<string, unknown>) => ({
		status: isOneOf(STATUS_KEYS, search.status) ? search.status : "DONE",
		view:
			search.view === "table" || search.view === "grid"
				? (search.view as ViewMode)
				: "grid",
		sort: isOneOf(SORT_KEYS, search.sort) ? search.sort : "newest",
		quality: parseStringParam(search.quality),
		channel: parseStringParam(search.channel),
		language: parseStringParam(search.language),
		duration: isOneOf(DURATION_FILTERS, search.duration)
			? search.duration
			: undefined,
		size: isOneOf(SIZE_FILTERS, search.size) ? search.size : undefined,
	}),
	component: VideosPage,
});

function VideosPage() {
	const { t } = useTranslation();
	const {
		status,
		view,
		sort: sortKey,
		quality,
		channel,
		language,
		duration,
		size,
	} = Route.useSearch();
	const navigate = Route.useNavigate();
	const [filtersOpen, setFiltersOpen] = useState(false);
	const loadMoreRef = useRef<HTMLDivElement | null>(null);
	const { data: stats } = useStatistics();
	const { data: channels } = useChannels();

	const sortConfig = SORT_CONFIG[sortKey];

	// "all" means "no status filter" on the wire — the server's
	// ListVideosOpts treats empty string as unfiltered. Any other
	// StatusKey is passed through verbatim.
	const videos = useInfiniteVideoPages(
		PAGE_SIZE,
		status === "all" ? undefined : status,
		sortConfig.sort,
		sortConfig.order,
		{
			quality,
			broadcasterId: channel,
			language,
			duration,
			size,
		},
	);
	const loadedRows = useMemo(
		() => videos.data?.pages.flatMap((page) => page.items) ?? [],
		[videos.data],
	);
	const hasScrolledThroughPages = (videos.data?.pages.length ?? 0) > 1;
	// Languages grow across the session so the dropdown doesn't
	// collapse to a single option once the user narrows the server
	// filter. Reset on page reload. See note in useLanguageFacet.
	const seenLanguages = useLanguageFacet(loadedRows, language);

	useEffect(() => {
		const node = loadMoreRef.current;
		if (!node || !videos.hasNextPage) {
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => {
				if (!entries[0]?.isIntersecting || videos.isFetchingNextPage) {
					return;
				}
				void videos.fetchNextPage();
			},
			{ rootMargin: "500px 0px" },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [videos.fetchNextPage, videos.hasNextPage, videos.isFetchingNextPage]);

	const columns = useMemo(() => videoListColumns(t), [t]);
	const statusLabel = (key: StatusKey) =>
		t(`videos.status_filter.${key === "all" ? "all" : key.toLowerCase()}`);
	const statusCount = (key: StatusKey) => {
		if (key === "all") return stats?.total ?? 0;
		return stats?.by_status.find((bucket) => bucket.status === key)?.count ?? 0;
	};
	const matchingTotal =
		status === "all"
			? (stats?.total ?? loadedRows.length)
			: (stats?.by_status.find((bucket) => bucket.status === status)?.count ??
				loadedRows.length);
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
	const channelOptions = useMemo(
		() =>
			withSelectedOption(
				[
					{ value: ANY, label: t("videos.filter_any") },
					...(channels ?? [])
						.map((c) => ({ value: c.broadcaster_id, label: channelLabel(c) }))
						.sort((a, b) => a.label.localeCompare(b.label)),
				],
				channel,
			),
		[channels, channel, t],
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
	const sizeOptions = useMemo(
		() => [
			{ value: ANY, label: t("videos.size_any") },
			{ value: "small", label: t("videos.size_small") },
			{ value: "medium", label: t("videos.size_medium") },
			{ value: "large", label: t("videos.size_large") },
		],
		[t],
	);

	// Client-side narrowing pass over the currently-mounted rows.
	// With placeholderData: keepPreviousData on the infinite query,
	// the old filter's rows stay mounted during the refetch — this
	// pass narrows them to match the new filter so the UI stays
	// responsive instead of showing stale wider data or a skeleton.
	// When the server responds, loadedRows becomes the authoritative
	// result and this pass becomes a no-op.
	const filteredVideos = useMemo(
		() =>
			loadedRows.filter((video) => {
				if (quality && video.quality !== quality) return false;
				if (channel && video.broadcaster_id !== channel) return false;
				if (language && video.language !== language) return false;
				if (!matchesDurationFilter(video.duration_seconds, duration)) {
					return false;
				}
				if (!matchesSizeFilter(video.size_bytes, size)) return false;
				return true;
			}),
		[loadedRows, quality, channel, language, duration, size],
	);
	const hasLocalFilters = !!(
		quality ||
		channel ||
		language ||
		duration ||
		size
	);
	// "Showing N of M loaded" is used whenever we can't cite a
	// server-wide matching total: when local filters are active
	// (stats aggregate doesn't reflect them) or during the
	// keepPreviousData transition window. Otherwise we use the
	// stats total for the current status bucket.
	const showingLabel =
		hasLocalFilters || videos.isPlaceholderData
			? t("videos.showing_loaded", {
					shown: filteredVideos.length,
					loaded: loadedRows.length,
				})
			: t("videos.showing", {
					shown: filteredVideos.length,
					total: matchingTotal,
				});
	const showEmpty =
		loadedRows.length === 0 && !videos.isLoading && !videos.error;
	const showNoMatchYet =
		loadedRows.length > 0 &&
		filteredVideos.length === 0 &&
		!videos.isLoading &&
		!videos.error;
	const emptyMessage = hasLocalFilters
		? t("videos.no_match")
		: t("videos.empty");
	const summary = stats
		? t("videos.summary", {
				count: stats.total.toLocaleString(),
				size: formatBytes(stats.total_size),
				channels: (channels?.length ?? 0).toLocaleString(),
			})
		: undefined;

	const setFilter = (patch: {
		quality?: string;
		channel?: string;
		language?: string;
		duration?: DurationFilter;
		size?: SizeFilter;
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
						variant="outline"
						onClick={() => setFiltersOpen((open) => !open)}
						className={cn(filtersOpen && "bg-accent text-accent-foreground")}
					>
						<FunnelSimple className="size-4" />
						{t("videos.filters")}
					</Button>
				</>
			}
		>
			<div className="space-y-6">
				<StatusTabs
					current={status}
					getCount={statusCount}
					getLabel={statusLabel}
					onChange={(next) => {
						void navigate({ search: (s) => ({ ...s, status: next }) });
					}}
				/>

				{filtersOpen ? (
					<div className="flex animate-in flex-col gap-3 fade-in-0 slide-in-from-top-1 duration-150 sm:flex-row sm:items-center sm:justify-between">
						<div className="flex flex-wrap items-center gap-2.5">
							<FilterChipSelect
								label={t("videos.filter_quality")}
								value={quality ?? ANY}
								options={qualityOptions}
								onChange={(value) =>
									setFilter({ quality: value === ANY ? undefined : value })
								}
							/>
							<FilterChipSelect
								label={t("videos.filter_channel")}
								value={channel ?? ANY}
								options={channelOptions}
								onChange={(value) =>
									setFilter({ channel: value === ANY ? undefined : value })
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
							<FilterChipSelect
								label={t("videos.filter_size")}
								value={size ?? ANY}
								options={sizeOptions}
								onChange={(value) =>
									setFilter({
										size: isOneOf(SIZE_FILTERS, value) ? value : undefined,
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

				{showEmpty && (
					<div className="rounded-xl border border-dashed border-border bg-card/50 px-6 py-12 text-center text-muted-foreground">
						{emptyMessage}
					</div>
				)}

				{showNoMatchYet && (
					<div className="rounded-xl border border-dashed border-border bg-card/50 px-6 py-12 text-center text-muted-foreground">
						{videos.hasNextPage || videos.isFetchingNextPage
							? t("videos.no_match_loaded")
							: t("videos.no_match")}
					</div>
				)}

				{filteredVideos.length > 0 &&
					(view === "grid" ? (
						<VirtualVideoGrid videos={filteredVideos} />
					) : (
						<DataTable
							columns={columns}
							data={filteredVideos}
							emptyMessage={t("videos.empty")}
							virtualizeRows
							estimateRowHeight={84}
						/>
					))}

				{loadedRows.length > 0 && !!videos.hasNextPage && !videos.error && (
					<div ref={loadMoreRef} className="h-1" />
				)}

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
					<SortAscending className="size-4 text-muted-foreground" />
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
		{ key: "grid", icon: <SquaresFour className="size-4" /> },
		{ key: "table", icon: <Rows className="size-4" /> },
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

function StatusTabs({
	current,
	getCount,
	getLabel,
	onChange,
}: {
	current: StatusKey;
	getCount: (key: StatusKey) => number;
	getLabel: (key: StatusKey) => string;
	onChange: (key: StatusKey) => void;
}) {
	return (
		<Tabs
			value={current}
			onValueChange={(value) => onChange(value as StatusKey)}
		>
			<div className="overflow-x-auto">
				<TabsList className="h-auto min-w-max justify-start gap-6 rounded-none border-b border-border bg-transparent p-0">
					{STATUS_KEYS.map((key) => (
						<TabsTrigger
							key={key}
							value={key}
							className={cn(
								"group relative h-auto cursor-pointer rounded-none px-0 pt-0 pb-4 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
								"data-[selected]:bg-transparent data-[selected]:text-foreground data-[selected]:shadow-none",
								"before:pointer-events-none before:absolute before:right-0 before:bottom-[-1px] before:left-0 before:h-0.5 before:rounded-full before:bg-primary before:opacity-0 before:transition-opacity",
								"data-[selected]:before:opacity-100",
							)}
						>
							<span>{getLabel(key)}</span>
							<span className="ml-2 text-xs text-muted-foreground transition-colors group-hover:text-foreground/70 group-data-[selected]:text-primary/80">
								{getCount(key).toLocaleString()}
							</span>
						</TabsTrigger>
					))}
				</TabsList>
			</div>
		</Tabs>
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

function matchesSizeFilter(
	bytes: number | undefined,
	filter: SizeFilter | undefined,
) {
	if (!filter) return true;
	if (!bytes || bytes <= 0) return false;
	if (filter === "small") return bytes < 1024 ** 3;
	if (filter === "medium") return bytes >= 1024 ** 3 && bytes < 4 * 1024 ** 3;
	return bytes >= 4 * 1024 ** 3;
}

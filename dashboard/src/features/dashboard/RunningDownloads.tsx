import { Link } from "@tanstack/react-router";
import { Fragment, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { ActiveDownloadResponse } from "@/api/generated/trpc";
import { Avatar } from "@/components/ui/avatar";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import {
	useDownloadCapacity,
	useLiveActiveDownloads,
	useVideoTimeline,
} from "@/features/videos";
import { TimelineChangeContent } from "@/features/videos/components/timelinePopover";
import { formatBytes, formatPlaybackTime } from "@/features/videos/format";
import { useLiveSeconds } from "@/hooks/useLiveSeconds";
import { cn, isFinitePositive, percentOf } from "@/lib/utils";
import {
	type ContentSegment,
	clampMetadataMarkers,
	contentSegmentsFromOrderedMarkers,
	orderedMetadataMarkers,
	recordingElapsedSeconds,
} from "./runningDownloadsTimeline";

// The live download pipeline, in execution order. The downloader walks these
// stages (see emitter.setStage(...) in downloader.go); the row renders them as
// a breadcrumb so an operator reads what's finished, what's running and what's
// left rather than a single opaque status word.
const STAGE_PIPELINE = [
	"auth",
	"playlist",
	"segments",
	"remux",
	"metadata",
	"thumbnail",
] as const;

export function RunningDownloads() {
	const { t } = useTranslation();
	const { data, dataUpdatedAt, isLoading, isError, error } =
		useLiveActiveDownloads();
	const { data: capacity } = useDownloadCapacity();
	const rows = data ?? [];
	const maxConcurrent = capacity?.max_concurrent ?? 0;

	return (
		<section className="rounded-2xl border border-border bg-card/70 px-4 py-4 sm:px-6 sm:py-5">
			<div className="flex items-center justify-between gap-4 border-b border-border pb-5">
				<h2 className="text-[1.35rem] font-medium tracking-tight text-foreground">
					{t("dashboard.running_now")}
				</h2>
				<div className="font-mono text-xs tracking-[0.16em] text-muted-foreground uppercase">
					{rows.length > 1 && maxConcurrent > 0
						? t("dashboard.active_capacity", {
								active: rows.length,
								max: maxConcurrent,
							})
						: t("dashboard.active_count", { count: rows.length })}
				</div>
			</div>

			{isLoading ? (
				<div className="pt-6 text-sm text-muted-foreground">
					{t("common.loading")}
				</div>
			) : isError && error ? (
				<div className="pt-6 text-sm text-destructive">
					{t("dashboard.running_now_failed")}: {error.message}
				</div>
			) : rows.length === 0 ? (
				<div className="pt-6 text-sm text-muted-foreground">
					{t("dashboard.running_now_empty")}
				</div>
			) : (
				<div className="divide-y divide-border">
					{rows.map((row) => (
						<RunningDownloadRow
							key={row.video.job_id}
							row={row}
							sampleAt={dataUpdatedAt}
						/>
					))}
				</div>
			)}
		</section>
	);
}

// One active recording, laid out as an instrument readout: identity + live
// throughput on top, the stage pipeline and metric line in the middle, and a
// slim part-timeline strip at the bottom whose details live in per-part hover
// popovers.
function RunningDownloadRow({
	row,
	sampleAt,
}: {
	row: ActiveDownloadResponse;
	sampleAt: number;
}) {
	const channelName = row.video.broadcaster_name || row.video.display_name;
	const estimatedBytes = estimateTotalBytes(row.bytes_written, row.percent);

	// Media-clock length of the recording so far. Drives the band widths and the
	// metadata-marker positions; everything on the strip shares this axis. When
	// the recording reports a live media offset we extrapolate it forward at 1x
	// between SSE samples so the elapsed clock (and the live band/popover that
	// ride it) keep ticking instead of freezing until the next push.
	const hasMediaClock = isFinitePositive(row.media_offset_seconds);
	const scaleSeconds = useLiveSeconds(
		recordingElapsedSeconds(row),
		sampleAt,
		hasMediaClock,
	);
	const hasTimeline = scaleSeconds > 0;

	// Poll the title/category timeline while the recording runs so a mid-stream
	// change shows up as a new segment. Disabled until there's a media axis.
	const { data: timeline } = useVideoTimeline(row.video.id, hasTimeline, {
		refetchInterval: 15_000,
	});
	const timelineMarkers = useMemo(
		() => orderedMetadataMarkers(timeline, row.video.start_download_at),
		[timeline, row.video.start_download_at],
	);
	const markers = useMemo(
		() =>
			hasTimeline ? clampMetadataMarkers(timelineMarkers, scaleSeconds) : [],
		[hasTimeline, scaleSeconds, timelineMarkers],
	);
	const segments = useMemo(
		() =>
			hasTimeline
				? contentSegmentsFromOrderedMarkers(markers, scaleSeconds)
				: [],
		[hasTimeline, markers, scaleSeconds],
	);
	// Parts are a storage/upload concern, not content — surfaced as a count in the
	// metric line rather than drawn on the (content) timeline. part_index is the
	// downloader's current part number (1-based), i.e. the count of parts so far.
	const partCount = hasTimeline ? Math.max(1, row.part_index || 1) : 0;
	// The current category + title — the last segment's metadata — shown as text
	// under the channel name (what's being recorded right now), mirroring the
	// "Just went live" card.
	const current = segments.at(-1);
	const currentLabel = [current?.category?.name, current?.title?.name]
		.filter(Boolean)
		.join(" · ");

	return (
		<div className="py-5">
			<div className="flex items-center justify-between gap-4">
				<div className="flex min-w-0 items-center gap-3">
					<Link
						to="/dashboard/channels/$channelId"
						params={{ channelId: row.video.broadcaster_id }}
						aria-label={channelName}
						className="shrink-0"
					>
						<Avatar
							src={row.video.profile_image_url}
							name={channelName}
							alt={channelName}
							size="md"
						/>
					</Link>
					<div className="min-w-0">
						<div className="flex min-w-0 items-center gap-2">
							{/* Static record indicator — recording is live, no pulse. */}
							<span
								aria-hidden="true"
								className="size-1.5 shrink-0 rounded-full bg-destructive"
							/>
							<Link
								to="/dashboard/channels/$channelId"
								params={{ channelId: row.video.broadcaster_id }}
								className="truncate text-[1.05rem] font-medium text-foreground hover:text-link"
							>
								{channelName}
							</Link>
						</div>
						{currentLabel ? (
							<div className="truncate text-sm text-muted-foreground">
								{currentLabel}
							</div>
						) : null}
					</div>
				</div>
				<div className="shrink-0 font-mono text-sm text-foreground tabular-nums">
					{row.speed || "—"}
				</div>
			</div>

			<div className="mt-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
				<StagePipeline stage={row.stage} />
				<DownloadMetrics
					row={row}
					scaleSeconds={scaleSeconds}
					estimatedBytes={estimatedBytes}
					partCount={partCount}
				/>
			</div>

			<div className="mt-3">
				<DownloadTimeline
					segments={segments}
					scaleSeconds={scaleSeconds}
					percent={row.percent}
				/>
			</div>
		</div>
	);
}

// StagePipeline renders the download lifecycle as a breadcrumb, highlighting the
// live stage with finished stages dimmed and upcoming ones faint.
function StagePipeline({ stage }: { stage: string }) {
	const rawIndex = STAGE_PIPELINE.indexOf(
		stage.toLowerCase() as (typeof STAGE_PIPELINE)[number],
	);
	// Stages emitted after the pipeline (store/done) or unknown tokens collapse to
	// "everything finished" so the breadcrumb never strands a stale live marker.
	const currentIndex = rawIndex === -1 ? STAGE_PIPELINE.length : rawIndex;

	return (
		<div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 font-mono text-[11px] tracking-tight">
			{STAGE_PIPELINE.map((name, index) => {
				const state =
					index < currentIndex
						? "past"
						: index === currentIndex
							? "current"
							: "future";
				return (
					<Fragment key={name}>
						{index > 0 ? (
							<span className="text-muted-foreground/30">›</span>
						) : null}
						<span
							className={cn(
								state === "current" && "text-primary",
								state === "past" && "text-muted-foreground",
								state === "future" && "text-muted-foreground/35",
							)}
						>
							{name}
						</span>
					</Fragment>
				);
			})}
		</div>
	);
}

// DownloadMetrics is the mono readout: only the figures that carry signal for a
// live recording, separated by middots. Open-ended recordings have no ETA, so
// that figure appears only when the server actually reports one.
function DownloadMetrics({
	row,
	scaleSeconds,
	estimatedBytes,
	partCount,
}: {
	row: ActiveDownloadResponse;
	scaleSeconds: number;
	estimatedBytes: number | null;
	partCount: number;
}) {
	const { t } = useTranslation();

	const figures: string[] = [];
	if (scaleSeconds > 0) figures.push(formatPlaybackTime(scaleSeconds));
	figures.push(
		estimatedBytes
			? `${formatBytes(row.bytes_written)} / ${formatBytes(estimatedBytes)}`
			: formatBytes(row.bytes_written),
	);
	figures.push(row.video.quality);
	// Part count only matters once the recording has actually split.
	if (partCount > 1) {
		figures.push(t("videos.parts_count", { count: partCount }));
	}
	if (row.segments_done > 0) {
		figures.push(t("dashboard.segments_done", { count: row.segments_done }));
	}
	if (row.segments_gaps > 0) {
		figures.push(t("dashboard.gaps_count", { count: row.segments_gaps }));
	}
	if (row.eta) figures.push(t("dashboard.eta_left", { eta: row.eta }));

	return (
		<div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs text-muted-foreground tabular-nums">
			{figures.map((figure, index) => (
				<Fragment key={figure}>
					{index > 0 ? (
						<span className="text-muted-foreground/40">·</span>
					) : null}
					<span>{figure}</span>
				</Fragment>
			))}
		</div>
	);
}

// DownloadTimeline draws the recording as content segments across the elapsed
// media clock: one band per period of constant metadata. Bands stay in the
// primary palette, shaded in two muted tones that flip on each category change —
// so a category reads as one shade across its title-change seams, adjacent
// categories always differ, and a single-category recording is just one solid
// bar. Each band's popover carries its category + title.
function DownloadTimeline({
	segments,
	scaleSeconds,
	percent,
}: {
	segments: ContentSegment[];
	scaleSeconds: number;
	percent: number;
}) {
	// No media axis yet (recording just started / no probed durations): fall back
	// to a plain percent bar so there's still a progress affordance.
	if (scaleSeconds <= 0 || segments.length === 0) {
		const width = percent > 0 ? `${Math.max(percent, 3)}%` : "3%";
		return (
			<div className="h-2.5 overflow-hidden rounded-full bg-muted/50">
				<div className="h-full rounded-full bg-primary/80" style={{ width }} />
			</div>
		);
	}

	const pct = (seconds: number) => `${percentOf(seconds, scaleSeconds)}%`;

	// Per-segment shade: flip the tone whenever the category changes from the
	// previous segment, so the shade tracks category runs (not raw index). A
	// title-only change keeps the same category id → same shade, marked only by
	// the seam between flex bands.
	const dimShade: boolean[] = [];
	let dim = false;
	for (let i = 0; i < segments.length; i++) {
		if (i > 0 && segments[i]?.category?.id !== segments[i - 1]?.category?.id) {
			dim = !dim;
		}
		dimShade.push(dim);
	}

	return (
		<TooltipProvider>
			<div className="relative h-2.5 w-full rounded-full bg-muted/50">
				<div className="absolute inset-0 flex gap-px overflow-hidden rounded-full">
					{segments.map((segment, index) => (
						<Tooltip key={segment.key} trackCursorAxis="x">
							<TooltipTrigger
								render={
									<div
										className={cn(
											"h-full flex-none cursor-default",
											dimShade[index] ? "bg-primary/55" : "bg-primary/80",
										)}
										style={{
											flexBasis: pct(segment.endSeconds - segment.startSeconds),
										}}
									/>
								}
							/>
							<TooltipContent side="top">
								<SegmentTooltip segment={segment} />
							</TooltipContent>
						</Tooltip>
					))}
				</div>

				{/* Static live edge — no pulse. */}
				<span
					aria-hidden="true"
					className="absolute top-1/2 right-0 h-3.5 w-0.5 -translate-y-1/2 rounded-full bg-primary"
				/>
			</div>
		</TooltipProvider>
	);
}

// SegmentTooltip is the per-segment popover: the segment's media range plus the
// category (with box art) and title in effect during it.
function SegmentTooltip({ segment }: { segment: ContentSegment }) {
	const { t } = useTranslation();
	return (
		<div className="space-y-2">
			<div className="font-mono text-[11px] text-muted-foreground">
				{t("watch.part_range", {
					start: formatPlaybackTime(segment.startSeconds),
					end: formatPlaybackTime(segment.endSeconds),
				})}
			</div>
			{segment.category || segment.title ? (
				<TimelineChangeContent
					tone="surface"
					change={{ category: segment.category, title: segment.title }}
				/>
			) : null}
		</div>
	);
}

function estimateTotalBytes(bytesWritten: number, percent: number) {
	if (bytesWritten <= 0 || percent <= 0 || percent > 100) return null;
	return Math.round(bytesWritten / (percent / 100));
}

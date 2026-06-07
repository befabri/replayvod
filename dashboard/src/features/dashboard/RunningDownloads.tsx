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
	useCancelDownload,
	useDownloadCapacity,
	useLiveActiveDownloads,
	useVideoTimeline,
} from "@/features/videos";
import { TimelineChangeContent } from "@/features/videos/components/timelinePopover";
import { WatchLaterButton } from "@/features/videos/components/WatchLaterButton";
import { formatBytes, formatPlaybackTime } from "@/features/videos/format";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useLiveSeconds } from "@/hooks/useLiveSeconds";
import { cn, isFinitePositive, percentOf } from "@/lib/utils";
import {
	type ContentSegment,
	clampMetadataMarkers,
	contentSegmentsFromOrderedMarkers,
	orderedMetadataMarkers,
	recordingElapsedSeconds,
} from "./runningDownloadsTimeline";

// Matches the downloader stage order and renders as the active-row breadcrumb.
const STAGE_PIPELINE = [
	"auth",
	"playlist",
	"segments",
	"remux",
	"metadata",
	"thumbnail",
] as const;

export function RunningDownloads({ limit }: { limit?: number }) {
	const { t } = useTranslation();
	const { data, dataUpdatedAt, isLoading, isError, error } =
		useLiveActiveDownloads();
	const { data: capacity } = useDownloadCapacity();
	const rows = data ?? [];
	const maxConcurrent = capacity?.max_concurrent ?? 0;
	const visible = limit != null ? rows.slice(0, limit) : rows;
	const hasMore = limit != null && rows.length > limit;

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
				<>
					<div className="divide-y divide-border">
						{visible.map((row) => (
							<RunningDownloadRow
								key={row.video.job_id}
								row={row}
								sampleAt={dataUpdatedAt}
								cancelable={limit == null}
							/>
						))}
					</div>
					{hasMore ? (
						<div className="pt-4">
							<Link
								to="/dashboard/downloads"
								className="text-sm text-link hover:underline"
							>
								{t("dashboard.view_all_downloads", { count: rows.length })}
							</Link>
						</div>
					) : null}
				</>
			)}
		</section>
	);
}

function RunningDownloadRow({
	row,
	sampleAt,
	cancelable,
}: {
	row: ActiveDownloadResponse;
	sampleAt: number;
	cancelable: boolean;
}) {
	const channelName = row.video.broadcaster_name || row.video.display_name;
	const estimatedBytes = estimateTotalBytes(row.bytes_written, row.percent);

	const hasMediaClock = isFinitePositive(row.media_offset_seconds);
	const scaleSeconds = useLiveSeconds(
		recordingElapsedSeconds(row),
		sampleAt,
		hasMediaClock,
	);
	const hasTimeline = scaleSeconds > 0;

	// Poll while the recording runs so mid-stream title/category changes appear.
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
	const partCount = hasTimeline ? Math.max(1, row.part_index || 1) : 0;
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
				<div className="flex shrink-0 items-center gap-3">
					<span className="font-mono text-sm text-foreground tabular-nums">
						{row.speed || "—"}
					</span>
					<WatchLaterButton
						videoId={row.video.id}
						watchLater={row.video.user_state?.watch_later ?? false}
					/>
					{cancelable ? (
						<CancelDownloadButton jobId={row.video.job_id} />
					) : null}
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

function CancelDownloadButton({ jobId }: { jobId: string }) {
	const { t } = useTranslation();
	const cancel = useCancelDownload();
	const canManage = useCanManageVideos();

	if (!canManage) {
		return null;
	}

	return (
		<button
			type="button"
			onClick={() => cancel.mutate({ job_id: jobId })}
			disabled={cancel.isPending}
			className="text-xs text-destructive hover:underline disabled:opacity-60"
		>
			{t("downloads.cancel")}
		</button>
	);
}

function StagePipeline({ stage }: { stage: string }) {
	const rawIndex = STAGE_PIPELINE.indexOf(
		stage.toLowerCase() as (typeof STAGE_PIPELINE)[number],
	);
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

function DownloadTimeline({
	segments,
	scaleSeconds,
	percent,
}: {
	segments: ContentSegment[];
	scaleSeconds: number;
	percent: number;
}) {
	if (scaleSeconds <= 0 || segments.length === 0) {
		const width = percent > 0 ? `${Math.max(percent, 3)}%` : "3%";
		return (
			<div className="h-2.5 overflow-hidden rounded-full bg-muted/50">
				<div className="h-full rounded-full bg-primary/80" style={{ width }} />
			</div>
		);
	}

	const pct = (seconds: number) => `${percentOf(seconds, scaleSeconds)}%`;

	// Shade tracks category runs, while title-only changes keep the same tone.
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

				<span
					aria-hidden="true"
					className="absolute top-1/2 right-0 h-3.5 w-0.5 -translate-y-1/2 rounded-full bg-primary"
				/>
			</div>
		</TooltipProvider>
	);
}

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

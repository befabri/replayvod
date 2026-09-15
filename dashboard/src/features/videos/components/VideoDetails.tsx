import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import type {
	VideoCategory,
	VideoResponse,
	VideoTitle,
} from "@/features/videos";
import {
	formatAverageBitrate,
	formatBytes,
	formatDuration,
	formatPlaybackTime,
} from "@/features/videos/format";
import { useVideoCategories, useVideoTitles } from "@/features/videos/queries";
import { dedupConsecutive } from "@/features/videos/timeline";
import { cn } from "@/lib/utils";

export function VideoMetaGrid({ video }: { video: VideoResponse }) {
	const { t } = useTranslation();

	const rows: Array<{ label: string; value: string }> = [];
	rows.push({
		label: t("videos.duration"),
		value: formatDuration(video.duration_seconds),
	});
	rows.push({ label: t("videos.size"), value: formatBytes(video.size_bytes) });
	rows.push({ label: t("videos.quality"), value: video.quality });
	if (video.size_bytes && video.duration_seconds)
		rows.push({
			label: t("videos.bitrate_avg"),
			value: formatAverageBitrate(video.size_bytes, video.duration_seconds),
		});
	if (video.language)
		rows.push({ label: t("videos.language"), value: video.language });
	const segments = countSegments(video);
	if (segments != null)
		rows.push({
			label: t("videos.segments"),
			value: segments.toLocaleString(),
		});
	rows.push({
		label: t("videos.started_at"),
		value: new Date(video.start_download_at).toLocaleString(),
	});
	if (video.downloaded_at)
		rows.push({
			label: t("videos.downloaded_at"),
			value: new Date(video.downloaded_at).toLocaleString(),
		});

	return (
		<Card className="overflow-hidden p-0 gap-0">
			<div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-px bg-border">
				{rows.map((row) => (
					<div key={row.label} className="bg-card px-4 py-3">
						<div className="text-[10px] uppercase tracking-[0.08em] text-muted-foreground mb-1.5">
							{row.label}
						</div>
						<div className="text-sm tabular-nums truncate" title={row.value}>
							{row.value}
						</div>
					</div>
				))}
			</div>
		</Card>
	);
}

export function CategoryTimelineCard({
	video,
	className,
}: {
	video: VideoResponse;
	className?: string;
}) {
	const { t } = useTranslation();
	const { data: categories } = useVideoCategories(video.id);

	const events = dedupConsecutive(categories ?? [], (c) => c.id);
	if (events.length === 0) return null;

	return (
		<Card className={cn("p-0 gap-0", className)}>
			<TimelineCardHeader
				heading={t("videos.category_history.heading")}
				count={events.length}
			/>
			<CardContent className="p-0">
				<ol className="flex flex-col divide-y divide-foreground/10">
					{events.map((category) => (
						<TimelineRow
							key={`${category.id}-${category.started_at}`}
							offsetSec={offsetSeconds(
								category.started_at,
								video.start_download_at,
							)}
						>
							<CategoryEvent category={category} />
						</TimelineRow>
					))}
				</ol>
			</CardContent>
		</Card>
	);
}

export function TitleTimelineCard({
	video,
	className,
}: {
	video: VideoResponse;
	className?: string;
}) {
	const { t } = useTranslation();
	const { data: titles } = useVideoTitles(video.id);

	const events = dedupConsecutive(titles ?? [], (x) => x.name);
	if (events.length === 0) return null;

	return (
		<Card className={cn("p-0 gap-0", className)}>
			<TimelineCardHeader
				heading={t("videos.title_history.heading")}
				count={events.length}
			/>
			<CardContent className="p-0">
				<ol className="flex flex-col divide-y divide-foreground/10">
					{events.map((title) => (
						<TimelineRow
							key={`${title.id}-${title.started_at}`}
							offsetSec={offsetSeconds(
								title.started_at,
								video.start_download_at,
							)}
							align="start"
						>
							<TitleEvent title={title} />
						</TimelineRow>
					))}
				</ol>
			</CardContent>
		</Card>
	);
}

function TimelineCardHeader({
	heading,
	count,
}: {
	heading: string;
	count: number;
}) {
	const { t } = useTranslation();
	return (
		<CardHeader className="flex-row items-center justify-between gap-3 p-5 pb-4 border-b border-foreground/10">
			<CardTitle className="text-sm font-medium uppercase tracking-wider">
				{heading}
			</CardTitle>
			<span className="shrink-0 text-xs tabular-nums text-foreground/60">
				{t("watch.events_count", { count })}
			</span>
		</CardHeader>
	);
}

function TimelineRow({
	offsetSec,
	align = "center",
	children,
}: {
	offsetSec: number;
	align?: "center" | "start";
	children: React.ReactNode;
}) {
	return (
		<li
			className={cn(
				"flex gap-3 px-5 py-3",
				align === "center" ? "items-center" : "items-start",
			)}
		>
			<span className="min-w-17 shrink-0 rounded-md bg-secondary px-2 py-1 text-center text-xs tabular-nums text-foreground/75">
				{formatPlaybackTime(offsetSec)}
			</span>
			{children}
		</li>
	);
}

function CategoryEvent({ category }: { category: VideoCategory }) {
	return (
		<Link
			to="/dashboard/categories/$categoryId"
			params={{ categoryId: category.id }}
			className="flex min-w-0 flex-1 items-center gap-3 rounded-md -mx-1 px-1 py-0.5 transition-colors hover:bg-accent/50"
		>
			<CategoryBoxArt
				url={category.box_art_url}
				name={category.name}
				width={40}
				height={54}
				className="w-10 rounded-md shrink-0"
			/>
			<span className="truncate text-sm font-medium">{category.name}</span>
		</Link>
	);
}

function TitleEvent({ title }: { title: VideoTitle }) {
	return (
		<div className="min-w-0 flex-1 text-sm leading-snug">{title.name}</div>
	);
}

function offsetSeconds(at: string, anchor: string): number {
	const ms = new Date(at).getTime() - new Date(anchor).getTime();
	return Math.max(0, Math.round(ms / 1000));
}

function countSegments(video: VideoResponse): number | null {
	if (!video.parts || video.parts.length === 0) return null;
	let total = 0;
	for (const p of video.parts) {
		const end = p.end_media_seq ?? p.start_media_seq;
		total += Math.max(0, end - p.start_media_seq + 1);
	}
	return total > 0 ? total : null;
}

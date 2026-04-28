import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
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
} from "@/features/videos/format";
import { useVideoCategories, useVideoTitles } from "@/features/videos/queries";
import { cn } from "@/lib/utils";

// VideoMetaGrid is the v1-reference meta block: a hairline-gutter grid
// of label/value cells. The "hairline" is the project's `border` token
// showing through 1px gaps between bg-card cells, so it remains
// visible regardless of light/dark theme — relying on the page bg
// being darker than the card surface only worked in dark mode.
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
	// HLS segment count: parts each cover a [start_media_seq,
	// end_media_seq] range. Total segments is the sum of those range
	// widths. parts is only populated by GetByID, which is what the
	// watch page calls — so it's available here.
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

// CategoryTimelineCard renders the category history as a v1-style
// timeline panel: dot + connecting line on the left, time-offset and
// content on the right. Uses video.start_download_at as the anchor so
// the offset of the first event is "Start" (or 00:00:00) and later
// events read as elapsed time relative to the recording, not relative
// to the first category change.
//
// Hidden when there's a single category — that's already in the
// VideoInfo chip strip (top-by-duration), so showing a one-row
// timeline would just duplicate it. The list dialog
// (StreamHistoryButton on VideoCard) takes the same "show only on
// change" stance: a single entry isn't history, it's just the
// current value.
export function CategoryTimelineCard({
	video,
	className,
}: {
	video: VideoResponse;
	className?: string;
}) {
	const { t } = useTranslation();
	const { data: categories } = useVideoCategories(video.id);

	// Run-length dedup: collapse consecutive spans for the same
	// category. The server returns one row per span so a stream that
	// flipped Delta Force → Delta Force (an event re-fire that didn't
	// actually change anything) shows up as two rows here. Without
	// dedup the timeline reads as a list of identical entries.
	const events = dedupConsecutive(categories ?? [], (c) => c.id);
	if (events.length === 0) return null;

	return (
		<Card className={cn("p-0 gap-0", className)}>
			<TimelineCardHeader
				heading={t("videos.category_history.heading")}
				count={events.length}
			/>
			<CardContent className="p-4">
				<ol className="flex flex-col">
					{events.map((category, idx) => (
						<TimelineRow
							key={`${category.id}-${category.started_at}`}
							offsetSec={offsetSeconds(
								category.started_at,
								video.start_download_at,
							)}
							isLast={idx === events.length - 1}
							dotClassName="bg-primary"
						>
							<CategoryEvent category={category} />
						</TimelineRow>
					))}
				</ol>
			</CardContent>
		</Card>
	);
}

// TitleTimelineCard mirrors CategoryTimelineCard for the title
// history. Same run-length dedup + "only show on real change" gate
// so a recording that never had a title change doesn't render a
// single-row timeline that just repeats VideoInfo's heading.
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
			<CardContent className="p-4">
				<ol className="flex flex-col">
					{events.map((title, idx) => (
						<TimelineRow
							key={`${title.id}-${title.started_at}`}
							offsetSec={offsetSeconds(
								title.started_at,
								video.start_download_at,
							)}
							isLast={idx === events.length - 1}
							dotClassName="bg-link"
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
		<CardHeader className="flex-row items-center justify-between p-4 pb-3 border-b border-foreground/10">
			<CardTitle className="text-sm uppercase tracking-wider text-muted-foreground font-medium">
				{heading}
			</CardTitle>
			<Badge variant="outline" className="tabular-nums">
				{t("watch.events_count", { count })}
			</Badge>
		</CardHeader>
	);
}

function TimelineRow({
	offsetSec,
	isLast,
	dotClassName,
	children,
}: {
	offsetSec: number;
	isLast: boolean;
	dotClassName: string;
	children: React.ReactNode;
}) {
	const { t } = useTranslation();
	const offsetLabel =
		offsetSec <= 0 ? t("videos.history.start") : formatDuration(offsetSec);
	return (
		<li className="flex gap-3">
			<div className="w-14 shrink-0 pt-1 text-right text-xs tabular-nums text-muted-foreground">
				{offsetLabel}
			</div>
			<div className="relative flex shrink-0 flex-col items-center">
				<span className={cn("mt-1.5 size-2 rounded-full", dotClassName)} />
				{!isLast && <span className="w-px flex-1 bg-foreground/10" />}
			</div>
			<div className="flex min-w-0 flex-1 flex-col gap-1 pb-4 last:pb-0">
				{children}
			</div>
		</li>
	);
}

function CategoryEvent({ category }: { category: VideoCategory }) {
	return (
		<Link
			// biome-ignore lint/suspicious/noExplicitAny: param route typing
			to={"/dashboard/categories/$categoryId" as any}
			// biome-ignore lint/suspicious/noExplicitAny: param route typing
			params={{ categoryId: category.id } as any}
			className="flex items-center gap-2.5 rounded-md hover:bg-accent/50 -mx-1 px-1 py-0.5 transition-colors"
		>
			<CategoryBoxArt
				url={category.box_art_url}
				name={category.name}
				width={28}
				height={36}
				className="w-7 rounded-sm shrink-0"
			/>
			<span className="truncate text-sm font-medium">{category.name}</span>
		</Link>
	);
}

function TitleEvent({ title }: { title: VideoTitle }) {
	return <div className="text-sm leading-snug">{title.name}</div>;
}

function offsetSeconds(at: string, anchor: string): number {
	const ms = new Date(at).getTime() - new Date(anchor).getTime();
	return Math.max(0, Math.round(ms / 1000));
}

// Run-length dedup: keeps the first row of every consecutive run with
// an equal key. Mirrors what a viewer would mentally do when reading a
// timeline of "Delta Force, Delta Force, Just Chatting, Delta Force"
// — three transitions, not four entries.
function dedupConsecutive<T>(items: T[], keyOf: (item: T) => unknown): T[] {
	const out: T[] = [];
	for (const item of items) {
		const last = out[out.length - 1];
		if (last && keyOf(last) === keyOf(item)) continue;
		out.push(item);
	}
	return out;
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

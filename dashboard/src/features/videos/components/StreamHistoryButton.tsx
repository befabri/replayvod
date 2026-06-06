import { InfoIcon, ListBulletsIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useState } from "react";
import type { TimelineEvent } from "@/api/generated/trpc";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useVideoTimeline } from "@/features/videos";
import { formatDuration } from "@/features/videos/format";
import {
	timelineEventKey,
	timelineEventOffsetSeconds,
} from "@/features/videos/timeline";

function dedupConsecutiveEvents(
	events: TimelineEvent[] | undefined,
): TimelineEvent[] | undefined {
	if (!events) return events;
	const out: TimelineEvent[] = [];
	for (const event of events) {
		const last = out[out.length - 1];
		if (
			last &&
			last.category?.id === event.category?.id &&
			last.title?.name === event.title?.name
		) {
			continue;
		}
		out.push(event);
	}
	return out;
}

// StreamHistoryButton renders the merged title + category timeline
// for a recording. The backing endpoint (video.timeline) reads from
// video_metadata_changes, where each row is one channel.update event
// with both dimensions captured under a single occurred_at — so the
// frontend doesn't need to interleave or group anything client-side.
//
// The query is gated on `open` (lazy-load): list pages won't fan out
// N requests at render time.
//
// `videoStartDownloadAt` is the recording's start instant. Each event's
// occurred_at is turned into a seconds offset from it, which is both the
// label and the deep-link target into the player (?t=offset) — clicking
// a row's timestamp jumps the watch page to that moment.
export function StreamHistoryButton({
	videoId,
	videoStartDownloadAt,
	t,
	className,
}: {
	videoId: number;
	videoStartDownloadAt: string;
	t: TFunction;
	className?: string;
}) {
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={(triggerProps) => (
					<button
						type="button"
						{...triggerProps}
						className={className}
						aria-label={t("videos.history.tooltip")}
						title={t("videos.history.tooltip")}
					>
						<ListBulletsIcon className="size-4" />
					</button>
				)}
			/>
			{open ? (
				<StreamHistoryDialogContent
					videoId={videoId}
					videoStartDownloadAt={videoStartDownloadAt}
					t={t}
					onNavigate={() => setOpen(false)}
				/>
			) : null}
		</Dialog>
	);
}

function StreamHistoryDialogContent({
	videoId,
	videoStartDownloadAt,
	t,
	onNavigate,
}: {
	videoId: number;
	videoStartDownloadAt: string;
	t: TFunction;
	onNavigate: () => void;
}) {
	const { data: rawEvents, isLoading } = useVideoTimeline(videoId, true);

	// Run-length dedup: collapse consecutive events whose (category,
	// title) pair is identical to the previous row. The server emits
	// one row per channel.update tick, so a re-fired event that didn't
	// change anything produces a row that's a literal duplicate of its
	// predecessor — visually noisy and not what a viewer thinks of as
	// "history."
	const events = dedupConsecutiveEvents(rawEvents);

	return (
		<DialogContent className="max-w-lg">
			<DialogHeader>
				<DialogTitle className="flex items-center gap-2">
					<span>{t("videos.history.heading")}</span>
					<TooltipProvider>
						<Tooltip>
							<TooltipTrigger
								render={
									<button
										type="button"
										className="inline-flex size-5 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
										aria-label={t("videos.history.description")}
									>
										<InfoIcon className="size-4" weight="regular" />
									</button>
								}
							/>
							<TooltipContent>{t("videos.history.description")}</TooltipContent>
						</Tooltip>
					</TooltipProvider>
				</DialogTitle>
				{/* Visually hidden — sighted users read the same text
					    from the InfoIcon tooltip; this connects the dialog
					    to a description for screen readers via Base UI's
					    auto-wired aria-describedby. */}
				<DialogDescription className="sr-only">
					{t("videos.history.description")}
				</DialogDescription>
			</DialogHeader>

			{isLoading && (
				<div className="text-muted-foreground text-sm py-4">
					{t("common.loading")}
				</div>
			)}

			{!isLoading && events && events.length === 0 && (
				<div className="text-muted-foreground text-sm py-4">
					{t("videos.history.empty")}
				</div>
			)}

			{events && events.length > 0 && (
				<ol className="flex flex-col py-2">
					{events.map((event, idx) => {
						// Use the same offset resolver the player seeks through
						// (media_offset_seconds when present, else wall-clock from the
						// recording start), so the timestamp link lands on the marker the
						// player shows — not ~the gap-length past it on a recording with
						// ad-break / dropped-segment gaps. Clamp the unparseable sentinel.
						const offsetSec = Math.max(
							0,
							timelineEventOffsetSeconds(event, videoStartDownloadAt),
						);
						// "Start" only when the change is actually at the recording's
						// start (offset 0). Keying on idx mislabelled the first row when
						// the earliest tracked change happened mid-recording.
						const offsetLabel =
							offsetSec === 0
								? t("videos.history.start")
								: formatDuration(offsetSec);
						const isLast = idx === events.length - 1;
						return (
							<li key={timelineEventKey(event)} className="flex gap-3">
								{/* Timestamp deep-links into the player at this offset. */}
								<Link
									to="/dashboard/watch/$videoId"
									params={{ videoId: String(videoId) }}
									search={{ t: offsetSec }}
									onClick={onNavigate}
									className="w-14 shrink-0 pt-1.5 text-right text-xs font-mono text-muted-foreground transition-colors hover:text-link"
								>
									{offsetLabel}
								</Link>
								<div className="relative flex shrink-0 flex-col items-center">
									<span className="mt-2 size-2 rounded-full bg-primary" />
									{!isLast && <span className="w-px flex-1 bg-border" />}
								</div>
								<div className="flex min-w-0 flex-1 flex-col gap-1.5 pb-4">
									{event.category && (
										<Link
											to="/dashboard/categories/$categoryId"
											params={{ categoryId: event.category.id }}
											className="flex items-center gap-2 rounded-md bg-muted/50 px-2 py-1.5 transition-colors hover:bg-accent"
										>
											<CategoryBoxArt
												url={event.category.box_art_url}
												name={event.category.name}
												width={28}
												height={36}
												className="w-7 rounded-sm shrink-0"
											/>
											<span className="truncate text-sm font-medium">
												{event.category.name}
											</span>
										</Link>
									)}
									{event.title && (
										<div className="text-sm leading-snug">
											{event.title.name}
										</div>
									)}
								</div>
							</li>
						);
					})}
				</ol>
			)}
		</DialogContent>
	);
}

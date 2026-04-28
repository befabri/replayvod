import { Info, ListBullets } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
export function StreamHistoryButton({
	videoId,
	className,
}: {
	videoId: number;
	className?: string;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const { data: rawEvents, isLoading } = useVideoTimeline(videoId, open);

	// Run-length dedup: collapse consecutive events whose (category,
	// title) pair is identical to the previous row. The server emits
	// one row per channel.update tick, so a re-fired event that didn't
	// change anything produces a row that's a literal duplicate of its
	// predecessor — visually noisy and not what a viewer thinks of as
	// "history."
	const events = dedupConsecutiveEvents(rawEvents);

	const firstEpoch =
		events && events.length > 0 ? new Date(events[0].occurred_at).getTime() : 0;

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
						<ListBullets className="size-4" />
					</button>
				)}
			/>
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
											<Info className="size-4" weight="regular" />
										</button>
									}
								/>
								<TooltipContent>
									{t("videos.history.description")}
								</TooltipContent>
							</Tooltip>
						</TooltipProvider>
					</DialogTitle>
					{/* Visually hidden — sighted users read the same text
					    from the Info tooltip; this connects the dialog
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
							const offsetSec = Math.max(
								0,
								Math.round(
									(new Date(event.occurred_at).getTime() - firstEpoch) / 1000,
								),
							);
							const offsetLabel =
								idx === 0
									? t("videos.history.start")
									: formatDuration(offsetSec);
							const isLast = idx === events.length - 1;
							return (
								<li key={`${event.occurred_at}-${idx}`} className="flex gap-3">
									<div className="w-14 shrink-0 pt-1.5 text-right text-xs font-mono text-muted-foreground">
										{offsetLabel}
									</div>
									<div className="relative flex shrink-0 flex-col items-center">
										<span className="mt-2 size-2 rounded-full bg-primary" />
										{!isLast && <span className="w-px flex-1 bg-border" />}
									</div>
									<div className="flex min-w-0 flex-1 flex-col gap-1.5 pb-4">
										{event.category && (
											<Link
												// biome-ignore lint/suspicious/noExplicitAny: param route typing
												to={"/dashboard/categories/$categoryId" as any}
												// biome-ignore lint/suspicious/noExplicitAny: param route typing
												params={{ categoryId: event.category.id } as any}
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
		</Dialog>
	);
}

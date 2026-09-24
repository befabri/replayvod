import { InfoIcon, ListBulletsIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useState } from "react";
import type { TimelineEvent } from "@/api/generated/trpc";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { LoadingState } from "@/components/ui/loading-state";
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

export function StreamHistoryButton({
	videoId,
	videoStartDownloadAt,
	t,
}: {
	videoId: number;
	videoStartDownloadAt: string;
	t: TFunction;
}) {
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={(triggerProps) => (
					<Button
						variant="ghost-muted"
						size="icon-sm"
						{...triggerProps}
						aria-label={t("videos.history.tooltip")}
						title={t("videos.history.tooltip")}
					>
						<ListBulletsIcon />
					</Button>
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
				<DialogDescription className="sr-only">
					{t("videos.history.description")}
				</DialogDescription>
			</DialogHeader>

			{isLoading && <LoadingState className="py-4" />}

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
							timelineEventOffsetSeconds(event, videoStartDownloadAt),
						);
						const offsetLabel =
							offsetSec === 0
								? t("videos.history.start")
								: formatDuration(offsetSec);
						const isLast = idx === events.length - 1;
						return (
							<li key={timelineEventKey(event)} className="flex gap-3">
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

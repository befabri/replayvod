import { InfoIcon, ListBulletsIcon } from "@phosphor-icons/react";
import type { TFunction } from "i18next";
import { useState } from "react";
import type { TimelineEvent } from "@/api/generated/trpc";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { useVideoTimeline } from "@/features/videos";
import {
	timelineEventKey,
	timelineEventOffsetSeconds,
} from "@/features/videos/timeline";
import { CategoryEvent, TimelineRow, TitleEvent } from "./TimelineRow";

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
		<DialogContent className="max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] max-w-lg grid-rows-[auto_minmax(0,1fr)]">
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

			<div className="min-h-0 overflow-y-auto">
				{isLoading && <LoadingState className="py-4" />}

				{!isLoading && events && events.length === 0 && (
					<div className="text-muted-foreground text-sm py-4">
						{t("videos.history.empty")}
					</div>
				)}

				{events && events.length > 0 && (
					<Card className="overflow-hidden p-0 gap-0">
						<ol className="flex flex-col divide-y divide-foreground/10">
							{events.map((event) => (
								<TimelineRow
									key={timelineEventKey(event)}
									align={event.category ? "center" : "baseline"}
									offsetSec={Math.max(
										0,
										timelineEventOffsetSeconds(event, videoStartDownloadAt),
									)}
									videoId={videoId}
									onNavigate={onNavigate}
								>
									<div className="flex min-w-0 flex-1 flex-col gap-2">
										{event.category && (
											<CategoryEvent category={event.category} />
										)}
										{event.title && <TitleEvent title={event.title} />}
									</div>
								</TimelineRow>
							))}
						</ol>
					</Card>
				)}
			</div>
		</DialogContent>
	);
}

import { Info, ListBullets } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
import {
	useVideoCategories,
	useVideoTitles,
	type VideoCategory,
	type VideoTitle,
} from "@/features/videos";
import { formatDuration } from "@/features/videos/format";

// StreamHistoryButton merges the title and category histories into a
// single chronological timeline. Streams change title and category
// independently — sometimes both at once, sometimes only one — so two
// separate dialogs forced viewers to mentally interleave the lists.
// One unified timeline grouped by `started_at` does that work for them.
//
// Both queries are gated on `open` (lazy-load): list pages won't fan
// out N requests at render time.
type TimelineGroup = {
	startedAt: string;
	title?: VideoTitle;
	category?: VideoCategory;
};

function buildTimeline(
	titles: VideoTitle[] | undefined,
	categories: VideoCategory[] | undefined,
): TimelineGroup[] {
	const groups = new Map<string, TimelineGroup>();
	const ensure = (startedAt: string): TimelineGroup => {
		let g = groups.get(startedAt);
		if (!g) {
			g = { startedAt };
			groups.set(startedAt, g);
		}
		return g;
	};
	for (const title of titles ?? []) {
		ensure(title.started_at).title = title;
	}
	for (const category of categories ?? []) {
		ensure(category.started_at).category = category;
	}
	return [...groups.values()].sort((a, b) =>
		a.startedAt.localeCompare(b.startedAt),
	);
}

export function StreamHistoryButton({
	videoId,
	className,
}: {
	videoId: number;
	className?: string;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const { data: titles, isLoading: loadingTitles } = useVideoTitles(
		videoId,
		open,
	);
	const { data: categories, isLoading: loadingCategories } = useVideoCategories(
		videoId,
		open,
	);

	const groups = buildTimeline(titles, categories);
	const isLoading = loadingTitles || loadingCategories;
	const firstEpoch =
		groups.length > 0 ? new Date(groups[0].startedAt).getTime() : 0;

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

				{!isLoading && groups.length === 0 && (
					<div className="text-muted-foreground text-sm py-4">
						{t("videos.history.empty")}
					</div>
				)}

				{groups.length > 0 && (
					<ol className="flex flex-col py-2">
						{groups.map((g, idx) => {
							const offsetSec = Math.max(
								0,
								Math.round(
									(new Date(g.startedAt).getTime() - firstEpoch) / 1000,
								),
							);
							const offsetLabel =
								idx === 0
									? t("videos.history.start")
									: formatDuration(offsetSec);
							const isLast = idx === groups.length - 1;
							return (
								<li key={g.startedAt} className="flex gap-3">
									<div className="w-14 shrink-0 pt-1.5 text-right text-xs font-mono text-muted-foreground">
										{offsetLabel}
									</div>
									<div className="relative flex shrink-0 flex-col items-center">
										<span className="mt-2 size-2 rounded-full bg-primary" />
										{!isLast && <span className="w-px flex-1 bg-border" />}
									</div>
									<div className="flex min-w-0 flex-1 flex-col gap-1.5 pb-4">
										{g.category && (
											<Link
												// biome-ignore lint/suspicious/noExplicitAny: param route typing
												to={"/dashboard/categories/$categoryId" as any}
												// biome-ignore lint/suspicious/noExplicitAny: param route typing
												params={{ categoryId: g.category.id } as any}
												className="flex items-center gap-2 rounded-md bg-muted/50 px-2 py-1.5 transition-colors hover:bg-accent"
											>
												<CategoryBoxArt
													url={g.category.box_art_url}
													name={g.category.name}
													width={28}
													height={36}
													className="w-7 rounded-sm shrink-0"
												/>
												<span className="truncate text-sm font-medium">
													{g.category.name}
												</span>
											</Link>
										)}
										{g.title && (
											<div className="text-sm leading-snug">{g.title.name}</div>
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
